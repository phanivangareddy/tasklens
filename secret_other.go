//go:build !darwin

package main

import "github.com/zalando/go-keyring"

// Windows Credential Manager and the Linux Secret Service. On both, programs
// running as the same user can read the entry; that is how those systems work.
func secretSet(service, user, pw string) error { return keyring.Set(service, user, pw) }

func secretGet(service, user string) (string, error) { return keyring.Get(service, user) }

func secretDelete(service, user string) { keyring.Delete(service, user) }
