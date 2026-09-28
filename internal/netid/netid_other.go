//go:build !windows && !darwin && !linux

package netid

// ssidPlatform 在不支持的平台上始终返回空串（= 未知网络）。
func ssidPlatform() string { return "" }
