// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main

import (
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/branding"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap/naming"
)

var (
	shortHelp = "Bootstrap a Ubuntu Core system"
	longHelp  = `
snap-bootstrap is a tool to bootstrap Ubuntu Core from ephemeral systems
such as initramfs.
`

	opts            struct{}
	commandBuilders []func(*flags.Parser)
)

func main() {
	// Load brand configuration early, before any other initialization.
	// For bootstrap, the config is expected in the initrd at /etc/snapd/snapd-config.yaml
	branding.LoadConfig()

	// Initialize subsystems that depend on branding configuration.
	sysdb.InitTrusted()
	sysdb.InitGeneric()
	naming.InitWellKnownSnapIDs()

	secboot.HijackAndRunArgon2OutOfProcessHandlerOnArg([]string{"argon2-proc"})

	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if os.Getuid() != 0 {
		return fmt.Errorf("please run as root")
	}
	logger.BootSetup()
	return parseArgs(args)
}

func parseArgs(args []string) error {
	p := parser()

	_, err := p.ParseArgs(args)
	if err != nil {
		logger.Noticef("execution error: %v", err)
	}
	return err
}

func parser() *flags.Parser {
	p := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	p.ShortDescription = shortHelp
	p.LongDescription = longHelp
	for _, builder := range commandBuilders {
		builder(p)
	}
	return p
}

func addCommandBuilder(builder func(*flags.Parser)) {
	commandBuilders = append(commandBuilders, builder)
}
