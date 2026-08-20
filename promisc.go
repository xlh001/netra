package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func setPromiscuous(ifaceName string) (alreadyOn bool, err error) {
	cur, err := getIfreqFlags(ifaceName)
	if err != nil {
		return false, err
	}
	if cur&unix.IFF_PROMISC != 0 {
		return true, nil
	}
	return false, setIfreqFlags(ifaceName, cur|unix.IFF_PROMISC)
}

func clearPromiscuous(ifaceName string) error {
	cur, err := getIfreqFlags(ifaceName)
	if err != nil {
		return err
	}
	return setIfreqFlags(ifaceName, cur&^unix.IFF_PROMISC)
}

func getIfreqFlags(ifaceName string) (uint16, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return 0, fmt.Errorf("open control socket: %w", err)
	}
	defer unix.Close(fd)

	ifr, err := unix.NewIfreq(ifaceName)
	if err != nil {
		return 0, fmt.Errorf("build ifreq for %s: %w", ifaceName, err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		return 0, fmt.Errorf("get flags for %s: %w", ifaceName, err)
	}
	return ifr.Uint16(), nil
}

func setIfreqFlags(ifaceName string, flags uint16) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("open control socket: %w", err)
	}
	defer unix.Close(fd)

	ifr, err := unix.NewIfreq(ifaceName)
	if err != nil {
		return fmt.Errorf("build ifreq for %s: %w", ifaceName, err)
	}
	ifr.SetUint16(flags)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, ifr); err != nil {
		return fmt.Errorf("set flags for %s: %w", ifaceName, err)
	}
	return nil
}
