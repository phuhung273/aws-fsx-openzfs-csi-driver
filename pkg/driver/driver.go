/*
Copyright 2023 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package driver

import (
	"context"
	"net"
	"sigs.k8s.io/aws-fsx-openzfs-csi-driver/pkg/driver/internal"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	"sigs.k8s.io/aws-fsx-openzfs-csi-driver/pkg/cloud"
	"sigs.k8s.io/aws-fsx-openzfs-csi-driver/pkg/util"
)

const (
	driverName = "fsx.openzfs.csi.aws.com"
)

var (
	volumeCaps = []csi.VolumeCapability_AccessMode{
		{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
		{
			Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
		},
	}
)

type Driver struct {
	endpoint string
	srv      *grpc.Server
	cloud    cloud.Cloud
	inFlight *internal.InFlight
	nodeID   string
	mounter  Mounter
}

func NewDriver(endpoint string) *Driver {
	metadata, err := cloud.NewMetadata()
	if err != nil {
		klog.Fatalln(err)
	}

	region := metadata.GetRegion()
	cloud := cloud.NewCloud(region)
	inFlight := internal.NewInFlight()
	return &Driver{
		endpoint: endpoint,
		cloud:    cloud,
		inFlight: inFlight,
		nodeID:   metadata.GetInstanceID(),
		mounter:  newNodeMounter(),
	}
}

func (d *Driver) Run() error {
	scheme, addr, err := util.ParseEndpoint(d.endpoint)
	if err != nil {
		return err
	}

	listener, err := net.Listen(scheme, addr)
	if err != nil {
		return err
	}

	logErr := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			klog.Errorf("GRPC error: %v", err)
		}
		return resp, err
	}
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(logErr),
	}
	d.srv = grpc.NewServer(opts...)

	csi.RegisterIdentityServer(d.srv, d)
	csi.RegisterControllerServer(d.srv, d)
	csi.RegisterNodeServer(d.srv, d)

	klog.Infof("Listening for connections on address: %#v", listener.Addr())
	return d.srv.Serve(listener)
}

func (d *Driver) Stop() {
	klog.Infof("Stopping server")
	d.srv.Stop()
}