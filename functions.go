package vxlan

import (
	"github.com/vishvananda/netlink"
)

func getHostInterface(vxlan *Vxlan) (*HostInterface, error) {
	var err error
	vxName := "vx_" + vxlan.Name
	mvName := "mv_" + vxlan.Name

	hi := &HostInterface{
		VxlanParams: vxlan,
		vxName:      vxName,
		mvName:      mvName,
	}

	hi.vxLink, err = netlink.LinkByName(vxName)
	if err != nil {
		return hi, err
	}

	hi.mvLink, err = netlink.LinkByName(mvName)
	if err != nil {
		return hi, err
	}

	return hi, nil
}

func linkIndexByName(name string) (int, error) {
	var i int
	dev, err := netlink.LinkByName(name)
	if err == nil {
		i = dev.Attrs().Index
	}
	return i, err
}
