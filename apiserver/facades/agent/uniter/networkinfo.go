// Copyright 2019 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package uniter

import (
	"strings"
	"time"

	"github.com/juju/clock"
	"github.com/juju/collections/set"
	"github.com/juju/errors"
	"github.com/juju/names/v4"
	"github.com/juju/retry"

	"github.com/juju/juju/apiserver/params"
	corenetwork "github.com/juju/juju/core/network"
	"github.com/juju/juju/network"
	"github.com/juju/juju/state"
)

type NetworkInfo interface {
	ProcessAPIRequest(params.NetworkInfoParams) (params.NetworkInfoResults, error)
	NetworksForRelation(
		binding string, rel *state.Relation, pollPublic bool,
	) (boundSpace string, ingress corenetwork.SpaceAddresses, egress []string, err error)

	init(unit *state.Unit) error
}

// TODO (manadart 2019-10-09):
// This module was pulled together out of the state package and the uniter API.
// It lacks sufficient coverage with direct unit tests, relying instead on the
// integration tests that were present at the time of its conception.
// Over time, the state types should be indirected, mocks generated, and
// appropriate tests added.

// NetworkInfoBase is responsible for processing requests for network data
// for unit endpoint bindings and/or relations.
type NetworkInfoBase struct {
	st *state.State
	// retryFactory returns a retry strategy template used to poll for
	// addresses that may not yet have landed in state,
	// such as for CAAS containers or HA syncing.
	retryFactory func() retry.CallArgs

	unit          *state.Unit
	app           *state.Application
	defaultEgress []string
	bindings      map[string]string
}

// NewNetworkInfo initialises and returns a new NetworkInfo
// based on the input state and unit tag.
func NewNetworkInfo(st *state.State, tag names.UnitTag, retryFactory func() retry.CallArgs) (NetworkInfo, error) {
	base := &NetworkInfoBase{
		st:           st,
		retryFactory: retryFactory,
	}

	unit, err := st.Unit(tag.Id())
	if err != nil {
		return nil, errors.Trace(err)
	}

	var netInfo NetworkInfo
	if unit.ShouldBeAssigned() {
		netInfo = &NetworkInfoIAAS{base}
	} else {
		netInfo = &NetworkInfoCAAS{base}
	}

	err = netInfo.init(unit)
	return netInfo, errors.Trace(err)
}

// init uses the member state to initialise NetworkInfoBase entities
// in preparation for the retrieval of network information.
func (n *NetworkInfoBase) init(unit *state.Unit) error {
	var err error

	n.unit = unit

	if n.app, err = n.unit.Application(); err != nil {
		return errors.Trace(err)
	}

	bindings, err := n.app.EndpointBindings()
	if err != nil {
		return errors.Trace(err)
	}
	n.bindings = bindings.Map()

	if n.defaultEgress, err = n.getModelEgressSubnets(); err != nil {
		return errors.Trace(err)
	}

	return nil
}

// getModelEgressSubnets returns model configuration for egress subnets.
func (n *NetworkInfoBase) getModelEgressSubnets() ([]string, error) {
	model, err := n.st.Model()
	if err != nil {
		return nil, errors.Trace(err)
	}
	cfg, err := model.ModelConfig()
	if err != nil {
		return nil, errors.Trace(err)
	}
	return cfg.EgressSubnets(), nil
}

// getRelationAndEndpointName returns the relation for the input ID
// and the name of the endpoint used by the relation.
func (n *NetworkInfoBase) getRelationAndEndpointName(relationID int) (*state.Relation, string, error) {
	rel, err := n.st.Relation(relationID)
	if err != nil {
		return nil, "", errors.Trace(err)
	}

	endpoint, err := rel.Endpoint(n.unit.ApplicationName())
	if err != nil {
		return nil, "", errors.Trace(err)
	}

	return rel, endpoint.Name, nil
}

// getRelationEgressSubnets returns the egress CIDRs for the input relation.
// If no networks are found, return the model default egress CIDRS.
func (n *NetworkInfoBase) getRelationEgressSubnets(rel *state.Relation) ([]string, error) {
	egressSubnets, err := state.NewRelationEgressNetworks(n.st).Networks(rel.Tag().Id())
	if err != nil {
		if errors.IsNotFound(err) {
			return n.defaultEgress, nil
		}
		return nil, errors.Trace(err)
	}

	return egressSubnets.CIDRS(), nil
}

// maybeGetUnitAddress returns an address for the member unit if either the
// input relation is cross-model and pollAddr is passed as true.
// The unit public address is preferred, but we will fall back to the private
// address if it does not become available in the polling window.
func (n *NetworkInfoBase) maybeGetUnitAddress(rel *state.Relation) (corenetwork.SpaceAddresses, error) {
	_, crossModel, err := rel.RemoteApplication()
	if err != nil {
		return nil, errors.Trace(err)
	}
	if !crossModel {
		return nil, nil
	}

	address, err := n.pollForAddress(n.unit.PublicAddress)
	if err != nil {
		logger.Warningf(
			"no public address for unit %q in cross model relation %q, will use private address", n.unit.Name(), rel)
	} else if address.Value != "" {
		return corenetwork.SpaceAddresses{address}, nil
	}

	address, err = n.pollForAddress(n.unit.PrivateAddress)
	if err != nil {
		logger.Warningf("no private address for unit %q in relation %q", n.unit.Name(), rel)
	} else if address.Value != "" {
		return corenetwork.SpaceAddresses{address}, nil
	}

	return nil, nil
}

func (n *NetworkInfoBase) pollForAddress(
	fetcher func() (corenetwork.SpaceAddress, error),
) (corenetwork.SpaceAddress, error) {
	var address corenetwork.SpaceAddress
	retryArg := n.retryFactory()
	retryArg.Func = func() error {
		var err error
		address, err = fetcher()
		return err
	}
	retryArg.IsFatalError = func(err error) bool {
		return !network.IsNoAddressError(err)
	}
	return address, retry.Call(retryArg)
}

func dedupNetworkInfoResults(info params.NetworkInfoResults) params.NetworkInfoResults {
	for epName, res := range info.Results {
		if res.Error != nil {
			continue
		}
		res.IngressAddresses = dedupStringListPreservingOrder(res.IngressAddresses)
		res.EgressSubnets = dedupStringListPreservingOrder(res.EgressSubnets)
		for infoIdx, info := range res.Info {
			res.Info[infoIdx].Addresses = dedupAddrList(info.Addresses)
		}
		info.Results[epName] = res
	}

	return info
}

func dedupStringListPreservingOrder(values []string) []string {
	// Ideally, we would use a set.Strings(values).Values() here but since
	// it does not preserve the insertion order we need to do this manually.
	seen := set.NewStrings()
	out := make([]string, 0, len(values))
	for _, v := range values {
		if seen.Contains(v) {
			continue
		}
		seen.Add(v)
		out = append(out, v)
	}

	return out
}

func dedupAddrList(addrList []params.InterfaceAddress) []params.InterfaceAddress {
	if len(addrList) <= 1 {
		return addrList
	}

	uniqueAddrList := make([]params.InterfaceAddress, 0, len(addrList))
	seenAddrSet := set.NewStrings()
	for _, addr := range addrList {
		if seenAddrSet.Contains(addr.Address) {
			continue
		}

		seenAddrSet.Add(addr.Address)
		uniqueAddrList = append(uniqueAddrList, addr)
	}

	return uniqueAddrList
}

// spaceAddressesFromNetworkInfo returns a SpaceAddresses collection
// from a slice of NetworkInfo.
// We need to construct sortable addresses from link-layer devices,
// which unlike addresses from the machines collection, do not have the scope
// information that we need.
// The best we can do here is identify fan addresses so that they are sorted
// after other addresses.
func spaceAddressesFromNetworkInfo(netInfos []network.NetworkInfo) corenetwork.SpaceAddresses {
	var addrs corenetwork.SpaceAddresses
	for _, nwInfo := range netInfos {
		scope := corenetwork.ScopeUnknown
		if strings.HasPrefix(nwInfo.InterfaceName, "fan-") {
			scope = corenetwork.ScopeFanLocal
		}

		for _, addr := range nwInfo.Addresses {
			addrs = append(addrs, corenetwork.NewScopedSpaceAddress(addr.Address, scope))
		}
	}
	return addrs
}

var defaultRetryFactory = func() retry.CallArgs {
	return retry.CallArgs{
		Clock:       clock.WallClock,
		Delay:       3 * time.Second,
		MaxDuration: 30 * time.Second,
	}
}
