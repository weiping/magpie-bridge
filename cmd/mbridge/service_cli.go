package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/weiping/magpie-bridge/internal/service"
)

var newService = service.New

// serviceCmd is `mbridge service install|uninstall|status|restart` (SP8 §5.7).
func serviceCmd(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: mbridge service install|uninstall|status|restart")
	}
	ctx := context.Background()
	m := newService()
	switch args[0] {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return err
		}
		if err := m.Install(ctx, exe); err != nil {
			return err
		}
		path, _ := service.Unit(m.GOOS, m.Home, m.ConfigDir, exe)
		fmt.Println(green.Render("✓"), "mbridge runs at login:", path)
		return nil
	case "uninstall":
		if err := m.Uninstall(ctx); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "mbridge no longer runs at login")
		return nil
	case "status":
		running, detail, err := m.Status(ctx)
		if err != nil {
			return err
		}
		mark := amber.Render("✗")
		if running {
			mark = green.Render("✓")
		}
		fmt.Println(mark, "service:", detail)
		return nil
	case "restart":
		// the running mbridge is the old binary: the supervisor brings the
		// new one up (SP14 §5.2)
		if err := m.Restart(ctx); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "mbridge restarts now")
		return nil
	}
	return fmt.Errorf("usage: mbridge service install|uninstall|status|restart")
}
