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

package model

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/protobuf/proto"

	multierror "github.com/hashicorp/go-multierror"

	proxyconfig "istio.io/api/proxy/v1/config"
)

const (
	dns1123LabelMaxLength int    = 63
	dns1123LabelFmt       string = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"
	// TODO: there is a stricter regex for the labels from validation.go in k8s
	qualifiedNameFmt string = "[-A-Za-z0-9_./]*"
)

var (
	dns1123LabelRex = regexp.MustCompile("^" + dns1123LabelFmt + "$")
	tagRegexp       = regexp.MustCompile("^" + qualifiedNameFmt + "$")
)

// IsDNS1123Label tests for a string that conforms to the definition of a label in
// DNS (RFC 1123).
func IsDNS1123Label(value string) bool {
	return len(value) <= dns1123LabelMaxLength && dns1123LabelRex.MatchString(value)
}

// Validate confirms that the names in the configuration key are appropriate
func (k *Key) Validate() error {
	var errs error
	if !IsDNS1123Label(k.Kind) {
		errs = multierror.Append(errs, fmt.Errorf("Invalid kind: %q", k.Kind))
	}
	if !IsDNS1123Label(k.Name) {
		errs = multierror.Append(errs, fmt.Errorf("Invalid name: %q", k.Name))
	}
	if !IsDNS1123Label(k.Namespace) {
		errs = multierror.Append(errs, fmt.Errorf("Invalid namespace: %q", k.Namespace))
	}
	return errs
}

// Validate checks that each name conforms to the spec and has a ProtoMessage
func (km KindMap) Validate() error {
	var errs error
	for k, v := range km {
		if !IsDNS1123Label(k) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid kind: %q", k))
		}
		if proto.MessageType(v.MessageName) == nil {
			errs = multierror.Append(errs, fmt.Errorf("Cannot find proto message type: %q", v.MessageName))
		}
	}
	return errs
}

// ValidateKey ensures that the key is well-defined and kind is well-defined
func (km KindMap) ValidateKey(k *Key) error {
	if err := k.Validate(); err != nil {
		return err
	}
	if _, ok := km[k.Kind]; !ok {
		return fmt.Errorf("Kind %q is not defined", k.Kind)
	}
	return nil
}

// ValidateConfig ensures that the config object is well-defined
func (km KindMap) ValidateConfig(k *Key, obj interface{}) error {
	if k == nil || obj == nil {
		return fmt.Errorf("Invalid nil configuration object")
	}

	if err := k.Validate(); err != nil {
		return err
	}
	t, ok := km[k.Kind]
	if !ok {
		return fmt.Errorf("Undeclared kind: %q", k.Kind)
	}

	v, ok := obj.(proto.Message)
	if !ok {
		return fmt.Errorf("Cannot cast to a proto message")
	}
	if proto.MessageName(v) != t.MessageName {
		return fmt.Errorf("Mismatched message type %q and kind %q",
			proto.MessageName(v), t.MessageName)
	}
	if err := t.Validate(v); err != nil {
		return err
	}

	return nil
}

// Validate ensures that the service object is well-defined
func (s *Service) Validate() error {
	var errs error
	if len(s.Hostname) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("Invalid empty hostname"))
	}
	parts := strings.Split(s.Hostname, ".")
	for _, part := range parts {
		if !IsDNS1123Label(part) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid hostname part: %q", part))
		}
	}

	// Require at least one port
	if len(s.Ports) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("Service must have at least one declared port"))
	}

	// Port names can be empty if there exists only one port
	for _, port := range s.Ports {
		if port.Name == "" {
			if len(s.Ports) > 1 {
				errs = multierror.Append(errs,
					fmt.Errorf("Empty port names are not allowed for services with multiple ports"))
			}
		} else if !IsDNS1123Label(port.Name) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid name: %q", port.Name))
		}
		if port.Port < 0 {
			errs = multierror.Append(errs, fmt.Errorf("Invalid service port value %d for %q", port.Port, port.Name))
		}
	}
	return errs
}

// Validate ensures that the service instance is well-defined
func (instance *ServiceInstance) Validate() error {
	var errs error
	if instance.Service == nil {
		errs = multierror.Append(errs, fmt.Errorf("Missing service in the instance"))
	} else if err := instance.Service.Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := instance.Tags.Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	if instance.Endpoint.Port < 0 {
		errs = multierror.Append(errs, fmt.Errorf("Negative port value: %d", instance.Endpoint.Port))
	}

	port := instance.Endpoint.ServicePort
	if port == nil {
		errs = multierror.Append(errs, fmt.Errorf("Missing service port"))
	} else if instance.Service != nil {
		expected, ok := instance.Service.Ports.Get(port.Name)
		if !ok {
			errs = multierror.Append(errs, fmt.Errorf("Missing service port %q", port.Name))
		} else {
			if expected.Port != port.Port {
				errs = multierror.Append(errs,
					fmt.Errorf("Unexpected service port value %d, expected %d", port.Port, expected.Port))
			}
			if expected.Protocol != port.Protocol {
				errs = multierror.Append(errs,
					fmt.Errorf("Unexpected service protocol %s, expected %s", port.Protocol, expected.Protocol))
			}
		}
	}

	return errs
}

// Validate ensures tag is well-formed
func (t Tags) Validate() error {
	var errs error
	for k, v := range t {
		if !tagRegexp.MatchString(k) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid tag key: %q", k))
		}
		if !tagRegexp.MatchString(v) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid tag value: %q", v))
		}
	}
	return errs
}

func validateFQDN(fqdn string) error {
	if len(fqdn) > 255 {
		return fmt.Errorf("domain name %q too long (max 255)", fqdn)
	}
	if len(fqdn) == 0 {
		return fmt.Errorf("empty domain name not allowed")
	}

	for _, label := range strings.Split(fqdn, ".") {
		if !IsDNS1123Label(label) {
			return fmt.Errorf("domain name %q invalid (label %q invalid)", fqdn, label)
		}
	}

	return nil
}

// ValidateMatchCondition validates a Match Condition
func ValidateMatchCondition(mc *proxyconfig.MatchCondition) (errs error) {

	if mc.Source != "" {
		if err := validateFQDN(mc.Source); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if err := Tags(mc.SourceTags).Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	if mc.GetTcp() != nil {
		if err := ValidateL4MatchAttributes(mc.GetTcp()); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if mc.GetUdp() != nil {
		if err := ValidateL4MatchAttributes(mc.GetUdp()); err != nil {
			errs = multierror.Append(errs, err)
		}
		errs = multierror.Append(errs, fmt.Errorf("Istio does not support UDP protocol yet"))
	}

	// TODO We do not (yet) validate http_headers.

	return
}

// ValidateL4MatchAttributes validates L4 Match Attributes
func ValidateL4MatchAttributes(ma *proxyconfig.L4MatchAttributes) (errs error) {

	for _, subnet := range ma.SourceSubnet {
		if err := validateSubnet(subnet); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	for _, subnet := range ma.DestinationSubnet {
		if err := validateSubnet(subnet); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return
}

func validatePercent(err error, val int32, label string) error {
	if val < 0 || val > 100 {
		err = multierror.Append(err, fmt.Errorf("%v must be in range 0..100", label))
	}
	return err
}

func validateFloatPercent(err error, val float32, label string) error {
	if val < 0.0 || val > 100.0 {
		err = multierror.Append(err, fmt.Errorf("%v must be in range 0..100", label))
	}
	return err
}

// ValidateDestinationWeight validates DestinationWeight
func ValidateDestinationWeight(dw *proxyconfig.DestinationWeight) (errs error) {

	if dw.Destination != "" {
		if err := validateFQDN(dw.Destination); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if err := Tags(dw.Tags).Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	errs = validatePercent(errs, dw.Weight, "weight")

	return
}

// ValidateHTTPTimeout validates HTTP Timeout
func ValidateHTTPTimeout(timeout *proxyconfig.HTTPTimeout) error {

	if simple := timeout.GetSimpleTimeout(); simple != nil {
		if simple.TimeoutSeconds < 0 {
			return fmt.Errorf("timeout_seconds must be in range [0..]")
		}

		// TODO validate override_header_name?
	}

	return nil
}

// ValidateHTTPRetries validates HTTP Retries
func ValidateHTTPRetries(retry *proxyconfig.HTTPRetry) (errs error) {

	if simple := retry.GetSimpleRetry(); simple != nil {
		if simple.Attempts < 0 {
			errs = multierror.Append(errs, fmt.Errorf("attempts must be in range [0..]"))
		}

		// We ignore override_header_name
	}

	return
}

// ValidateHTTPFault validates HTTP Fault
func ValidateHTTPFault(fault *proxyconfig.HTTPFaultInjection) (errs error) {

	if fault.GetDelay() != nil {
		if err := validateDelay(fault.GetDelay()); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if fault.GetAbort() != nil {
		if err := validateAbort(fault.GetAbort()); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return
}

// ValidateL4Fault validates L4 Fault
func ValidateL4Fault(fault *proxyconfig.L4FaultInjection) (errs error) {

	if fault.GetTerminate() != nil {
		if err := validateTerminate(fault.GetTerminate()); err != nil {
			errs = multierror.Append(errs, err)
		}
		errs = multierror.Append(errs, fmt.Errorf("Istio does not support the terminate fault yet"))
	}

	if fault.GetThrottle() != nil {
		if err := validateThrottle(fault.GetThrottle()); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return
}

func validateSubnet(subnet string) error {
	// The current implementation only supports IP v4 addresses
	return validateIPv4Subnet(subnet)
}

// validateIPv4Subnet validates that a string in "CIDR notation" or "Dot-decimal notation"
func validateIPv4Subnet(subnet string) error {

	// We expect a string in "CIDR notation" or "Dot-decimal notation"
	// E.g., a.b.c.d/xx form or just a.b.c.d
	parts := strings.Split(subnet, "/")
	if len(parts) > 2 {
		return fmt.Errorf("%q is not valid CIDR notation", subnet)
	}

	var errs error

	if len(parts) == 2 {
		if err := validateCIDRBlock(parts[1]); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if err := validateIPv4Address(parts[0]); err != nil {
		errs = multierror.Append(errs, err)
	}

	return errs
}

// validateCIDRBlock validates that a string in "CIDR notation" or "Dot-decimal notation"
func validateCIDRBlock(cidr string) error {
	if bits, err := strconv.Atoi(cidr); err != nil || bits <= 0 || bits > 32 {
		return fmt.Errorf("/%v is not a valid CIDR block", cidr)
	}

	return nil
}

// validateIPv4Address validates that a string in "CIDR notation" or "Dot-decimal notation"
func validateIPv4Address(addr string) error {
	octets := strings.Split(addr, ".")
	if len(octets) != 4 {
		return fmt.Errorf("%q is not a valid IP address", addr)
	}

	for _, octet := range octets {
		if n, err := strconv.Atoi(octet); err != nil || n < 0 || n > 255 {
			return fmt.Errorf("%q is not a valid IP address", addr)
		}
	}

	return nil
}

func validateDelay(delay *proxyconfig.HTTPFaultInjection_Delay) (errs error) {

	errs = validateFloatPercent(errs, delay.Percent, "delay")

	if delay.GetFixedDelaySeconds() < 0 {
		errs = multierror.Append(errs, fmt.Errorf("delay fixed_seconds invalid"))
	}

	if delay.GetExponentialDelaySeconds() != 0 {
		if delay.GetExponentialDelaySeconds() < 0 {
			errs = multierror.Append(errs, fmt.Errorf("delay exponential_seconds invalid"))
		}
		errs = multierror.Append(errs, fmt.Errorf("Istio does not support exponential_seconds yet"))
	}

	return
}

func validateAbortHTTPStatus(httpStatus *proxyconfig.HTTPFaultInjection_Abort_HttpStatus) (errs error) {

	if httpStatus.HttpStatus < 0 || httpStatus.HttpStatus > 600 {
		errs = multierror.Append(errs, fmt.Errorf("invalid abort http status %v", httpStatus.HttpStatus))
	}

	return
}

func validateAbort(abort *proxyconfig.HTTPFaultInjection_Abort) (errs error) {

	errs = validateFloatPercent(errs, abort.Percent, "abort")

	switch abort.ErrorType.(type) {
	case *proxyconfig.HTTPFaultInjection_Abort_GrpcStatus:
		// TODO No validation yet for grpc_status / http2_error / http_status
		errs = multierror.Append(errs, fmt.Errorf("Istio does not support gRPC fault injection yet"))
	case *proxyconfig.HTTPFaultInjection_Abort_Http2Error:
		// TODO No validation yet for grpc_status / http2_error / http_status
	case *proxyconfig.HTTPFaultInjection_Abort_HttpStatus:
		if err := validateAbortHTTPStatus(abort.ErrorType.(*proxyconfig.HTTPFaultInjection_Abort_HttpStatus)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	// No validation yet for override_header_name

	return
}

func validateTerminate(terminate *proxyconfig.L4FaultInjection_Terminate) (errs error) {

	errs = validateFloatPercent(errs, terminate.Percent, "terminate")

	return
}

func validateThrottle(throttle *proxyconfig.L4FaultInjection_Throttle) (errs error) {

	errs = validateFloatPercent(errs, throttle.Percent, "throttle")

	if throttle.DownstreamLimitBps < 0 {
		errs = multierror.Append(errs, fmt.Errorf("downstream_limit_bps invalid"))
	}

	if throttle.UpstreamLimitBps < 0 {
		errs = multierror.Append(errs, fmt.Errorf("upstream_limit_bps invalid"))
	}

	if throttle.GetThrottleAfterSeconds() < 0 {
		errs = multierror.Append(errs, fmt.Errorf("throttle_after_seconds invalid"))
	}

	if throttle.GetThrottleAfterBytes() < 0 {
		errs = multierror.Append(errs, fmt.Errorf("throttle_after_bytes invalid"))
	}

	// TODO Check DoubleValue throttle.GetThrottleForSeconds()

	return
}

// ValidateLoadBalancing validates Load Balancing
func ValidateLoadBalancing(lb *proxyconfig.LoadBalancing) (errs error) {

	// Currently the policy is just a name, and we don't validate it

	return
}

// ValidateCircuitBreaker validates Circuit Breaker
func ValidateCircuitBreaker(cb *proxyconfig.CircuitBreaker) (errs error) {

	if simple := cb.GetSimpleCb(); simple != nil {
		if simple.MaxConnections < 0 {
			errs = multierror.Append(errs, fmt.Errorf("circuit_breaker max_connections must be in range [0..]"))
		}
		if simple.HttpMaxPendingRequests < 0 {
			errs = multierror.Append(errs, fmt.Errorf("circuit_breaker max_pending_requests must be in range [0..]"))
		}
		if simple.HttpMaxRequests < 0 {
			errs = multierror.Append(errs, fmt.Errorf("circuit_breaker max_requests must be in range [0..]"))
		}
		if simple.SleepWindowSeconds < 0 {
			errs = multierror.Append(errs, fmt.Errorf("circuit_breaker sleep_window_seconds must be in range [0..]"))
		}
		if simple.HttpConsecutiveErrors < 0 {
			errs = multierror.Append(errs, fmt.Errorf("circuit_breaker http_consecutive_errors must be in range [0..]"))
		}
		if simple.HttpDetectionIntervalSeconds < 0 {
			errs = multierror.Append(errs,
				fmt.Errorf("circuit_breaker http_detection_interval_seconds must be in range [0..]"))
		}
		if simple.HttpMaxRequestsPerConnection < 0 {
			errs = multierror.Append(errs,
				fmt.Errorf("circuit_breaker http_max_requests_per_connection must be in range [0..]"))
		}
		errs = validatePercent(errs, simple.HttpMaxEjectionPercent, "circuit_breaker http_max_ejection_percent")
	}

	return
}

func validateWeights(routes []*proxyconfig.DestinationWeight, defaultDestination string) (errs error) {

	// Sum weights
	sum := 0
	for _, destWeight := range routes {
		sum = sum + int(destWeight.Weight)
	}

	// From cfg.proto "If there is only [one] destination in a rule, the weight value is assumed to be 100."
	if len(routes) == 1 && sum == 0 {
		return
	}

	if sum != 100 {
		errs = multierror.Append(errs,
			fmt.Errorf("Route weights total %v (must total 100)", sum))
	}

	return
}

// ValidateRouteRule checks routing rules
func ValidateRouteRule(msg proto.Message) error {

	value, ok := msg.(*proxyconfig.RouteRule)
	if !ok {
		return fmt.Errorf("cannot cast to routing rule")
	}

	var errs error
	if value.Destination == "" {
		errs = multierror.Append(errs, fmt.Errorf("route rule must have a destination service"))
	}
	if err := validateFQDN(value.Destination); err != nil {
		errs = multierror.Append(errs, err)
	}

	// We don't validate precedence because any int32 is legal

	if value.Match != nil {
		if err := ValidateMatchCondition(value.Match); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.Route != nil {
		for _, destWeight := range value.Route {
			if err := ValidateDestinationWeight(destWeight); err != nil {
				errs = multierror.Append(errs, err)
			}
		}
		if err := validateWeights(value.Route, value.Destination); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.HttpReqTimeout != nil {
		if err := ValidateHTTPTimeout(value.HttpReqTimeout); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.HttpReqRetries != nil {
		if err := ValidateHTTPRetries(value.HttpReqRetries); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.HttpFault != nil {
		if err := ValidateHTTPFault(value.HttpFault); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.L4Fault != nil {
		if err := ValidateL4Fault(value.L4Fault); err != nil {
			errs = multierror.Append(errs, err)
		}
		errs = multierror.Append(errs, fmt.Errorf("L4 faults are not implemented"))
	}

	return errs
}

// ValidateIngressRule checks ingress rules
func ValidateIngressRule(msg proto.Message) error {
	// TODO: Add ingress-only validation checks, if any?
	return ValidateRouteRule(msg)
}

// ValidateDestinationPolicy checks proxy policies
func ValidateDestinationPolicy(msg proto.Message) error {
	value, ok := msg.(*proxyconfig.DestinationPolicy)
	if !ok {
		return fmt.Errorf("Cannot cast to destination policy")
	}

	var errs error

	if value.Destination == "" {
		errs = multierror.Append(errs,
			fmt.Errorf("destination policy should have a valid service name in its destination field"))
	} else {
		if err := validateFQDN(value.Destination); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if err := Tags(value.Tags).Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	if value.GetLoadBalancing() != nil {
		if err := ValidateLoadBalancing(value.GetLoadBalancing()); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.GetCircuitBreaker() != nil {
		if err := ValidateCircuitBreaker(value.GetCircuitBreaker()); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return errs
}
