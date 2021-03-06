// Copyright (C) 2021 Storj Labs, Inc.
// See LICENSE for copying information.

package main

import (
	"context"

	"github.com/zeebo/clingy"

	"storj.io/uplink"
)

type projectProvider struct {
	access      string
	openProject func(ctx context.Context) (*uplink.Project, error)
}

func (pp *projectProvider) Setup(a clingy.Arguments, f clingy.Flags) {
	pp.access = f.New("access", "Which access to use", "").(string)
}

func (pp *projectProvider) OpenProject(ctx context.Context) (*uplink.Project, error) {
	if pp.openProject != nil {
		return pp.openProject(ctx)
	}

	accessDefault, accesses, err := gf.GetAccessInfo()
	if err != nil {
		return nil, err
	}
	if pp.access != "" {
		accessDefault = pp.access
	}

	var access *uplink.Access
	if data, ok := accesses[accessDefault]; ok {
		access, err = uplink.ParseAccess(data)
	} else {
		access, err = uplink.ParseAccess(accessDefault)
	}
	if err != nil {
		return nil, err
	}
	return uplink.OpenProject(ctx, access)
}
