// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package envoy

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/manager/model"
	"istio.io/manager/proxy"
)

// Watcher observes service registry and triggers a reload on a change
type Watcher interface {
	Run(stop <-chan struct{})
}

// ProxyContext defines local proxy context information about the service mesh
type ProxyContext struct {
	// Discovery interface for listing services and instances
	Discovery model.ServiceDiscovery
	// Config interface for listing routing rules
	Config *model.IstioRegistry
	// MeshConfig defines global configuration settings
	MeshConfig *proxyconfig.ProxyMeshConfig
	// IPAddress is the IP address of the proxy used to identify it and its co-located service instances
	IPAddress string
}

type watcher struct {
	agent   proxy.Agent
	context *ProxyContext
	ctl     model.Controller
}

// NewWatcher creates a new watcher instance with an agent
func NewWatcher(discovery model.ServiceDiscovery, ctl model.Controller,
	registry *model.IstioRegistry, mesh *proxyconfig.ProxyMeshConfig, ipAddress string) (Watcher, error) {
	glog.V(2).Infof("Local instance address: %s", ipAddress)

	// Use proxy node IP as the node name
	// This parameter is used as the value for "service-node"
	agent := proxy.NewAgent(runEnvoy(mesh, ipAddress), cleanupEnvoy(mesh), 10, 100*time.Millisecond)

	out := &watcher{
		agent: agent,
		context: &ProxyContext{
			Discovery:  discovery,
			Config:     registry,
			MeshConfig: mesh,
			IPAddress:  ipAddress,
		},
		ctl: ctl,
	}

	// Initialize envoy according to the current model state,
	// instead of waiting for the first event to arrive.
	// Note that this is currently done synchronously (blocking),
	// to avoid racing with controller events lurking around the corner.
	// This can be improved once we switch to a mechanism where reloads
	// are linearized (e.g., by a single goroutine reloader).
	// TODO: this blocks
	//	out.reload()

	if err := ctl.AppendServiceHandler(func(*model.Service, model.Event) { out.reload() }); err != nil {
		return nil, err
	}

	// TODO: restrict the notification callback to co-located instances (e.g. with the same IP)
	// TODO: editing pod tags directly does not trigger instance handlers, we need to listen on pod resources.
	if err := ctl.AppendInstanceHandler(func(*model.ServiceInstance, model.Event) { out.reload() }); err != nil {
		return nil, err
	}

	handler := func(model.Key, proto.Message, model.Event) { out.reload() }

	if err := ctl.AppendConfigHandler(model.RouteRule, handler); err != nil {
		return nil, err
	}

	if err := ctl.AppendConfigHandler(model.DestinationPolicy, handler); err != nil {
		return nil, err
	}

	return out, nil
}

func (w *watcher) Run(stop <-chan struct{}) {
	// must start consumer before producer
	go w.agent.Run(stop)
	w.ctl.Run(stop)
}

func (w *watcher) reload() {
	// TODO
	// even though the function is called on every modification event,
	// the actual config is generated from the latest cache view
	config := Generate(w.context)
	w.agent.ScheduleConfigUpdate(config)
}

const (
	// EpochFileTemplate is a template for the root config JSON
	EpochFileTemplate = "%s/envoy-rev%d.json"

	// BinaryPath is the path to envoy binary
	BinaryPath = "/usr/local/bin/envoy"

	// ConfigPath is the directory to hold enovy epoch configurations
	ConfigPath = "/etc/envoy"
)

func configFile(config string, epoch int) string {
	return fmt.Sprintf(EpochFileTemplate, config, epoch)
}

func runEnvoy(mesh *proxyconfig.ProxyMeshConfig, ip string) func(interface{}, int) error {
	return func(config interface{}, epoch int) error {
		envoyConfig, ok := config.(*Config)
		if !ok {
			return fmt.Errorf("Unexpected config type: %#v", config)
		}

		// attempt to write file
		fname := configFile(ConfigPath, epoch)
		if err := envoyConfig.WriteFile(fname); err != nil {
			return err
		}

		// spin up a new Envoy process
		args := []string{"-c", fname,
			"--restart-epoch", fmt.Sprint(epoch),
			"--drain-time-s", fmt.Sprint(int(convertDuration(mesh.DrainDuration) / time.Second)),
			"--parent-shutdown-time-s", fmt.Sprint(int(convertDuration(mesh.ParentShutdownDuration) / time.Second)),
			"--service-cluster", mesh.IstioServiceCluster,
			"--service-node", ip,
		}

		// inject tracing flag for higher levels
		if glog.V(4) {
			args = append(args, "-l", "trace")
		} else if glog.V(3) {
			args = append(args, "-l", "debug")
		}

		glog.V(2).Infof("Envoy command: %v", args)

		/* #nosec */
		cmd := exec.Command(BinaryPath, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		return cmd.Run()
	}
}

func cleanupEnvoy(mesh *proxyconfig.ProxyMeshConfig) func(int) {
	return func(epoch int) {
		path := configFile(ConfigPath, epoch)
		if err := os.Remove(path); err != nil {
			glog.Warningf("Failed to delete config file %s for %d, %v", path, epoch, err)
		}
	}
}
