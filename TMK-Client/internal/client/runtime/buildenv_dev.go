//go:build !production

package runtimeconfig

const (
	defaultRuntimeEnvironment = EnvTest
	productionBuild           = false
)
