package main

import "fmt"

// errNotImplemented is a placeholder returned by command stubs that are not yet
// wired up. Each milestone replaces these with real implementations.
func errNotImplemented(cmd string) error {
	return fmt.Errorf("%q not implemented yet", cmd)
}
