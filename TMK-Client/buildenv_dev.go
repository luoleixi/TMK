//go:build !production

package main

const (
	defaultRuntimeEnvironment = envTest
	productionBuild           = false
)
