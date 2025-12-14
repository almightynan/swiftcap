package portal

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

func StartScreencast() error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("E_PORTAL_DENIED=11: Failed to connect to D-Bus session: %w", err)
	}

	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")

	// create a session to cast the screen
	var sessionHandle dbus.ObjectPath
	err = obj.Call("org.freedesktop.portal.ScreenCast.CreateSession", 0, map[string]map[string]interface{}{}).Store(&sessionHandle)
	if err != nil {
		return fmt.Errorf("E_PORTAL_DENIED=11: CreateSession failed: %w", err)
	}

	// SelectSources (monitor, cursor)
	var selectSourcesHandle dbus.ObjectPath
	selectSourcesOpts := map[string]map[string]interface{}{
		"sources": {
			"types":       uint32(1), // 1=monitor, 2=window
			"multiple":    false,
			"cursor_mode": uint32(2), // 2=metadata
		},
	}
	err = obj.Call("org.freedesktop.portal.ScreenCast.SelectSources", 0, sessionHandle, selectSourcesOpts).Store(&selectSourcesHandle)
	if err != nil {
		return fmt.Errorf("E_PORTAL_DENIED=11: SelectSources failed: %w", err)
	}

	// start
	var startResult map[string]dbus.Variant
	err = obj.Call("org.freedesktop.portal.ScreenCast.Start", 0, sessionHandle, map[string]map[string]interface{}{}).Store(&startResult)
	if err != nil {
		return fmt.Errorf("E_PORTAL_DENIED=11: Start failed: %w", err)
	}

	// get PipeWire node ID
	nodes, ok := startResult["streams"]
	if !ok {
		return fmt.Errorf("E_PORTAL_DENIED=11: No streams returned")
	}
	streams := nodes.Value().([]map[string]interface{})
	if len(streams) == 0 {
		return fmt.Errorf("E_PORTAL_DENIED=11: No PipeWire streams available")
	}
	nodeID, ok := streams[0]["node_id"].(uint32)
	if !ok {
		return fmt.Errorf("E_PORTAL_DENIED=11: PipeWire node_id missing")
	}

	fmt.Printf("Wayland screencast PipeWire node ID: %d\n", nodeID)
	return nil
}
