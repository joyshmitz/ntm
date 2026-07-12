// Package robot provides machine-readable output for AI agents.
// proxy_status.go implements the --robot-proxy-status command.
package robot

import (
	"context"
	"fmt"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tools"
)

// ProxyStatusOutput represents the response from --robot-proxy-status.
type ProxyStatusOutput struct {
	RobotResponse
	Proxy ProxyStatusInfo `json:"proxy"`
}

// ProxyStatusInfo contains rust_proxy daemon status and route metrics.
type ProxyStatusInfo struct {
	DaemonRunning  bool                     `json:"daemon_running"`
	Version        string                   `json:"version,omitempty"`
	ListenPort     int                      `json:"listen_port,omitempty"`
	UptimeSeconds  int64                    `json:"uptime_seconds,omitempty"`
	Routes         []ProxyRouteInfo         `json:"routes,omitempty"`
	FailoverEvents []ProxyFailoverEventInfo `json:"failover_events,omitempty"`
}

// ProxyRouteInfo describes a configured/active proxy route.
type ProxyRouteInfo struct {
	Domain   string `json:"domain,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Active   bool   `json:"active"`
	BytesIn  int64  `json:"bytes_in,omitempty"`
	BytesOut int64  `json:"bytes_out,omitempty"`
	Requests int64  `json:"requests,omitempty"`
	Errors   int64  `json:"errors,omitempty"`
}

// ProxyFailoverEventInfo captures historical failover records.
type ProxyFailoverEventInfo struct {
	Timestamp string `json:"timestamp,omitempty"`
	Domain    string `json:"domain,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// GetProxyStatus returns rust_proxy status information.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetProxyStatus() (*ProxyStatusOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adapter := tools.NewProxyAdapter()
	availability, err := adapter.GetAvailability(ctx)
	if err != nil {
		return &ProxyStatusOutput{
			RobotResponse: NewErrorResponse(err, ErrCodeInternalError, "Failed to check rust_proxy availability"),
			Proxy:         ProxyStatusInfo{DaemonRunning: false},
		}, nil
	}
	if availability == nil || !availability.Available {
		return &ProxyStatusOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("rust_proxy not installed"),
				ErrCodeDependencyMissing,
				"Install rust_proxy and ensure it is on PATH",
			),
			Proxy: ProxyStatusInfo{DaemonRunning: false},
		}, nil
	}
	if !availability.Compatible {
		return &ProxyStatusOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("rust_proxy version incompatible"),
				ErrCodeDependencyMissing,
				"Update rust_proxy to a compatible version",
			),
			Proxy: ProxyStatusInfo{
				DaemonRunning: false,
				Version:       availability.Version.String(),
			},
		}, nil
	}

	status, err := adapter.GetStatus(ctx)
	if err != nil {
		return &ProxyStatusOutput{
			RobotResponse: NewErrorResponse(err, ErrCodeInternalError, "Failed to query rust_proxy status"),
			Proxy: ProxyStatusInfo{
				DaemonRunning: availability.Running,
				Version:       availability.Version.String(),
			},
		}, nil
	}

	return &ProxyStatusOutput{
		RobotResponse: NewRobotResponse(true),
		Proxy:         buildProxyStatusInfo(status, availability),
	}, nil
}

// PrintProxyStatus handles the --robot-proxy-status command.
// This is a thin wrapper around GetProxyStatus() for CLI output.
func PrintProxyStatus() error {
	output, err := GetProxyStatus()
	if err != nil {
		return err
	}
	return encodeTerminalRobotOutput(output, output.RobotResponse, "robot proxy status failed")
}

func buildProxyStatusInfo(status *tools.ProxyStatus, availability *tools.ProxyAvailability) ProxyStatusInfo {
	info := ProxyStatusInfo{}
	if availability != nil {
		info.Version = availability.Version.String()
		info.DaemonRunning = availability.Running
	}
	if status == nil {
		return info
	}

	if status.Version != "" {
		info.Version = status.Version
	}
	info.DaemonRunning = status.Running
	info.ListenPort = status.ListenPort
	info.UptimeSeconds = status.UptimeSeconds
	info.Routes = buildProxyRouteInfos(status)
	info.FailoverEvents = buildProxyFailoverInfos(status.FailoverEvents)

	return info
}

func buildProxyRouteInfos(status *tools.ProxyStatus) []ProxyRouteInfo {
	if status == nil {
		return nil
	}
	if len(status.RouteStats) > 0 {
		routes := make([]ProxyRouteInfo, 0, len(status.RouteStats))
		for _, route := range status.RouteStats {
			routes = append(routes, ProxyRouteInfo{
				Domain:   route.Domain,
				Upstream: route.Upstream,
				Active:   route.Active,
				BytesIn:  route.BytesIn,
				BytesOut: route.BytesOut,
				Requests: route.Requests,
				Errors:   route.Errors,
			})
		}
		return routes
	}
	if status.Routes <= 0 {
		return nil
	}

	// Preserve count information even when route details are unavailable.
	return make([]ProxyRouteInfo, status.Routes)
}

func buildProxyFailoverInfos(events []tools.ProxyFailoverEvent) []ProxyFailoverEventInfo {
	if len(events) == 0 {
		return nil
	}
	result := make([]ProxyFailoverEventInfo, 0, len(events))
	for _, event := range events {
		result = append(result, ProxyFailoverEventInfo{
			Timestamp: event.Timestamp,
			Domain:    event.Domain,
			From:      event.From,
			To:        event.To,
			Reason:    event.Reason,
		})
	}
	return result
}
