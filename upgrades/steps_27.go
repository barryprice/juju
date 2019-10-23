// Copyright 2019 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package upgrades

import (
	"os"
	"path/filepath"

	"github.com/juju/errors"
	"github.com/juju/utils/series"
	"gopkg.in/juju/names.v3"

	"github.com/juju/juju/agent"
	k8sprovider "github.com/juju/juju/caas/kubernetes/provider"
	"github.com/juju/juju/core/paths"
	"github.com/juju/juju/service"
)

// stateStepsFor27 returns upgrade steps for Juju 2.7.0.
func stateStepsFor27() []Step {
	return []Step{
		&upgradeStep{
			description: "add controller node docs",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().AddControllerNodeDocs()
			},
		},
		&upgradeStep{
			description: "recreate spaces with IDs",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().AddSpaceIdToSpaceDocs()
			},
		},
		&upgradeStep{
			description: "change subnet AvailabilityZone to AvailabilityZones",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().ChangeSubnetAZtoSlice()
			},
		},
		&upgradeStep{
			description: "change subnet SpaceName to SpaceID",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().ChangeSubnetSpaceNameToSpaceID()
			},
		},
		&upgradeStep{
			description: "recreate subnets with IDs",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().AddSubnetIdToSubnetDocs()
			},
		},
		&upgradeStep{
			description: "replace portsDoc.SubnetID as a CIDR with an ID.",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().ReplacePortsDocSubnetIDCIDR()
			},
		},
		&upgradeStep{
			description: "ensure application settings exist for all relations",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().EnsureRelationApplicationSettings()
			},
		},
		&upgradeStep{
			description: "ensure stored addresses refer to space by ID, and remove old space name/provider ID",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().ConvertAddressSpaceIDs()
			},
		},
		&upgradeStep{
			description: "adds default value for default_space",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().AddDefaultSpaceSetting()
			},
		},
		&upgradeStep{
			description: "replace space name in endpointBindingDoc bindings with an space ID",
			targets:     []Target{DatabaseMaster},
			run: func(context Context) error {
				return context.State().ReplaceSpaceNameWithIDEndpointBindings()
			},
		},
	}
}

// stepsFor27 returns upgrade steps for Juju 2.7.
func stepsFor27() []Step {
	return []Step{
		&upgradeStep{
			description: "change owner of unit and machine logs to adm",
			targets:     []Target{AllMachines},
			run:         resetLogPermissions,
		},
	}
}

func setJujuFolderPermissionsToAdm(dir string) error {
	wantedOwner, wantedGroup := paths.SyslogUserGroup()

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return errors.Trace(err)
		}
		if info.IsDir() {
			return nil
		}
		fullPath := dir + string(os.PathSeparator) + info.Name()
		if err := paths.SetOwnership(fullPath, wantedOwner, wantedGroup); err != nil {
			return errors.Trace(err)
		}
		if err := os.Chmod(fullPath, paths.LogfilePermission); err != nil {
			return errors.Trace(err)
		}
		return nil
	})
	if err != nil {
		return errors.Trace(err)
	}
	logger.Infof("Successfully changed permissions of dir %q", dir)
	return nil
}

// We rewrite/reset the systemd files and change the existing log file permissions
func resetLogPermissions(context Context) error {
	tag := context.AgentConfig().Tag()
	if tag.Kind() != names.MachineTagKind {
		logger.Infof("skipping agent %q, not a machine", tag.String())
		return nil
	}

	// For now a CAAS cannot be machineTagKind so it will not come as far as here for k8.
	// But to make sure for future refactoring, which are planned, we check here as well.
	if context.AgentConfig().Value(agent.ProviderType) == k8sprovider.CAASProviderType {
		logger.Infof("skipping agent %q, is CAAS", k8sprovider.CAASProviderType)
		return nil
	}
	isSystemd, err := getCurrentInit()
	if err != nil {
		return errors.Trace(err)
	}
	if !isSystemd {
		logger.Infof("skipping update of log file ownership as host not using systemd")
		return nil
	}
	sysdManager := service.NewServiceManagerWithDefaults()
	if err = sysdManager.WriteServiceFiles(); err != nil {
		return errors.Trace(err)
	}
	logDir := context.AgentConfig().LogDir()
	if err = setJujuFolderPermissionsToAdm(logDir); err != nil {
		return errors.Trace(err)
	}
	logger.Infof("Successfully wrote service files in /lib/systemd/system path")
	return nil
}

func getCurrentInit() (bool, error) {
	hostSeries, err := series.HostSeries()
	if err != nil {
		return false, errors.Trace(err)
	}
	initName, err := service.VersionInitSystem(hostSeries)
	if err != nil {
		return false, errors.Trace(err)
	}
	if initName == service.InitSystemSystemd {
		return true, nil
	} else {
		return false, nil
	}
}
