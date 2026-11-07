/*
 * Copyright (c) 2026 PenEngineering S.R.L
 * SPDX-License-Identifier: Apache-2.0
 */

package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/PenEngineering/akira-cli/internal/transport"
	"github.com/spf13/cobra"
)

// Device-management commands (info, logs, apps) that talk to the on-device REST
// API on port 8080. The app developer workflow (new, build, deploy --sim,
// devices/mDNS, publish) lives in the Python `akira` CLI; these mirror the
// device side here so the Go tool is self-sufficient for a connected board.

var (
	devDevice  string
	devToken   string
	devTimeout int
)

// normDevice ensures a host[:port] has the default device port.
func normDevice(d string) string {
	if d == "" {
		return d
	}
	hp := strings.TrimPrefix(strings.TrimPrefix(d, "http://"), "https://")
	if !strings.Contains(hp, ":") {
		return d + ":8080"
	}
	return d
}

func addDeviceFlags(c *cobra.Command) {
	c.Flags().StringVar(&devDevice, "device", "", "device IP or host[:port] (required)")
	c.Flags().StringVar(&devToken, "token", "", "OTA bearer token, if the device requires one")
	c.Flags().IntVar(&devTimeout, "timeout", 15, "HTTP timeout in seconds")
	_ = c.MarkFlagRequired("device")
}

func deviceDo(method, path string) error {
	cl := transport.NewHTTPClient(normDevice(devDevice), devToken, devTimeout)
	status, body, err := cl.Device(method, path)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", devDevice, err)
	}
	if strings.TrimSpace(body) != "" {
		fmt.Fprintln(os.Stdout, strings.TrimRight(body, "\n"))
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("device returned HTTP %d", status)
	}
	return nil
}

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Print a device's /api/v1/info",
	Args:  cobra.NoArgs,
	RunE:  func(cmd *cobra.Command, args []string) error { return deviceDo("GET", "/api/v1/info") },
}

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Fetch a device's recent logs (/api/v1/logs)",
	Args:  cobra.NoArgs,
	RunE:  func(cmd *cobra.Command, args []string) error { return deviceDo("GET", "/api/v1/logs") },
}

var appsCmd = &cobra.Command{
	Use:   "apps <list|start|stop|remove> [name]",
	Short: "List or control apps on a device",
	Long: `Manage the apps installed on a connected AkiraOS device.

  akira-cli apps list   --device 192.168.1.42
  akira-cli apps start  snake --device 192.168.1.42
  akira-cli apps stop   snake --device 192.168.1.42
  akira-cli apps remove snake --device 192.168.1.42`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		action := args[0]
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		switch action {
		case "list":
			return deviceDo("GET", "/api/v1/apps")
		case "start", "stop":
			if name == "" {
				return fmt.Errorf("apps %s needs an app name", action)
			}
			return deviceDo("POST", "/api/v1/apps/"+action+"?name="+url.QueryEscape(name))
		case "remove":
			if name == "" {
				return fmt.Errorf("apps remove needs an app name")
			}
			return deviceDo("DELETE", "/api/v1/apps?name="+url.QueryEscape(name))
		default:
			return fmt.Errorf("unknown action %q (use list|start|stop|remove)", action)
		}
	},
}

func init() {
	for _, c := range []*cobra.Command{infoCmd, logsCmd, appsCmd} {
		addDeviceFlags(c)
		rootCmd.AddCommand(c)
	}
}
