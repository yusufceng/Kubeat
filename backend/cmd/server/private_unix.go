//go:build unix

package main

import "syscall"

// privateFiles makes the files the server creates from now on readable and
// writable by its own user only: the database with its journal files, and
// the backups. They hold the accounts' password hashes, the check history
// and, without a key, the secrets of monitors and channels. Nothing else
// the server writes is meant for another user.
func privateFiles() { syscall.Umask(0o077) }
