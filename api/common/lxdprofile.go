// Copyright 2018 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"github.com/juju/errors"
	names "gopkg.in/juju/names.v2"

	"github.com/juju/juju/api/base"
	apiwatcher "github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/core/watcher"
)

// LXDProfileAPI provides common agent-side API functions
type LXDProfileAPI struct {
	facade base.FacadeCaller
	tag    names.Tag
}

// NewLXDProfileAPI creates a LXDProfileAPI on the specified facade,
// and uses this name when calling through the caller.
func NewLXDProfileAPI(facade base.FacadeCaller, tag names.Tag) *LXDProfileAPI {
	return &LXDProfileAPI{facade: facade, tag: tag}
}

// WatchLXDProfileUpgradeNotifications returns a NotifyWatcher for observing the state of
// a LXD profile upgrade
func (u *LXDProfileAPI) WatchLXDProfileUpgradeNotifications() (watcher.NotifyWatcher, error) {
	var results params.NotifyWatchResults
	args := params.Entities{
		Entities: []params.Entity{{Tag: u.tag.String()}},
	}
	err := u.facade.FacadeCall("WatchLXDProfileUpgradeNotifications", args, &results)
	if err != nil {
		return nil, err
	}
	if len(results.Results) != 1 {
		return nil, errors.Errorf("expected 1 result, got %d", len(results.Results))
	}
	result := results.Results[0]
	if result.Error != nil {
		return nil, result.Error
	}
	w := apiwatcher.NewNotifyWatcher(u.facade.RawAPICaller(), result)
	return w, nil
}

// UpgradeCharmProfileUnitStatus returns the lxd profile status of a
// unit from remote state.
func (u *LXDProfileAPI) UpgradeCharmProfileUnitStatus() ([]string, error) {
	var results params.UpgradeCharmProfileStatusResults
	args := params.Entities{
		Entities: []params.Entity{{Tag: u.tag.String()}},
	}

	err := u.facade.FacadeCall("UpgradeCharmProfileUnitStatus", args, &results)
	if err != nil {
		return nil, err
	}
	statuses := make([]string, len(results.Results))
	for i, res := range results.Results {
		if res.Error != nil {
			if params.IsCodeNotFound(res.Error) {
				return nil, errors.NewNotFound(res.Error, "")
			}
			return nil, res.Error
		}
		statuses[i] = res.Status
	}
	return statuses, nil
}

// RemoveUpgradeCharmProfileData removes the lxd profile status instance data
// for a machine
func (u *LXDProfileAPI) RemoveUpgradeCharmProfileData() error {
	var results params.ErrorResults
	args := params.Entities{
		Entities: []params.Entity{{Tag: u.tag.String()}},
	}

	err := u.facade.FacadeCall("RemoveUpgradeCharmProfileData", args, &results)
	if err != nil {
		return err
	}
	return results.OneError()
}
