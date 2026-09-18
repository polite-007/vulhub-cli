// Package hostinfo 提供本机可用于外部访问的 IP。
package hostinfo

import (
	"errors"
	"net"
	"strings"
)

// Info 提供本机 IP。
type Info interface {
	// LocalIP 返回第一块非环回、非容器相关的网卡地址。
	LocalIP() (string, error)
}

// System 通过枚举本机网卡实现 Info。
type System struct{}

// containerIfacePrefixes 是要跳过的网卡名前缀。
// 共享或 Docker 宿主机上这些地址对队友没有意义，他们访问不到。
var containerIfacePrefixes = []string{"docker", "br-", "veth", "virbr", "lo"}

// LocalIP 返回第一块非环回、非容器相关的 IPv4 地址。
func (System) LocalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isContainerInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue
			}
			return ip4.String(), nil
		}
	}
	return "", errors.New("找不到可用于外部访问的网卡地址")
}

func isContainerInterface(name string) bool {
	for _, prefix := range containerIfacePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
