//go:build !unix

package main

// privateFiles has nothing to set where files have no Unix permissions.
func privateFiles() {}
