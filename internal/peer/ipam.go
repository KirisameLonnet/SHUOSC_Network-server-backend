package peer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
)

var ErrNoIPAvailable = errors.New("no IP addresses available")

type IPAM interface {
	Allocate(ctx context.Context) (string, error)
	Release(ctx context.Context, ip string) error
	Reserve(ip string) error
}

type IPAMImpl struct {
	subnet *net.IPNet
	used   map[string]bool
	mu     sync.Mutex
}

func NewIPAM(subnetCIDR string) (*IPAMImpl, error) {
	_, ipNet, err := net.ParseCIDR(subnetCIDR)
	if err != nil {
		return nil, err
	}

	return &IPAMImpl{
		subnet: ipNet,
		used:   make(map[string]bool),
	}, nil
}

func (i *IPAMImpl) Allocate(_ context.Context) (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	ones, _ := i.subnet.Mask.Size()
	hostBits := 32 - ones

	networkIP := i.subnet.IP.To4()
	if networkIP == nil {
		return "", ErrNoIPAvailable
	}

	networkNum := uint32(networkIP[0])<<24 | uint32(networkIP[1])<<16 | uint32(networkIP[2])<<8 | uint32(networkIP[3])

	totalHosts := uint64(1) << hostBits

	for idx := uint64(1); idx < totalHosts-1; idx++ {
		ipNum := networkNum + uint32(idx)
		ip := net.IP{byte(ipNum >> 24), byte(ipNum >> 16), byte(ipNum >> 8), byte(ipNum)}.String()
		if !i.used[ip] {
			i.used[ip] = true
			return ip, nil
		}
	}

	return "", ErrNoIPAvailable
}

func (i *IPAMImpl) Release(_ context.Context, ip string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	delete(i.used, ip)
	return nil
}

func (i *IPAMImpl) Reserve(ip string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return fmt.Errorf("invalid ip %q", ip)
	}
	if !i.subnet.Contains(parsed) {
		return fmt.Errorf("ip %q is outside subnet %s", ip, i.subnet.String())
	}

	i.used[parsed.String()] = true
	return nil
}

var _ IPAM = (*IPAMImpl)(nil)
