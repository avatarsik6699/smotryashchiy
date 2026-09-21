//go:build !linux

package collect

func statfs(string) (FSStat, error) { return FSStat{}, errUnsupported }
