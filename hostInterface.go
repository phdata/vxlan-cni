package vxlan

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/TrilliumIT/iputil"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// HostInterface represents the host's connection to the vxlan
// It must be made up of both a "vxlan" interface which participates in the cluster's vxlan
// and a "macvlan" slave of the vxlan interface which acts as the hosts connection to that vxlan
// the hosts address gets isntalled on the macvlan
type HostInterface struct {
	VxlanParams *Vxlan
	vxLink      netlink.Link
	vxName      string
	mvLink      netlink.Link
	mvName      string
}

// GetOrCreateHostInterface creates required host interfaces if they don't exist, or gets them if they already do
func GetOrCreateHostInterface(vxlan *Vxlan) (*HostInterface, error) {
	hi, _ := getHostInterface(vxlan)
	gateway := hi.GetGateway()

	if hi.vxLink != nil && hi.mvLink != nil && hi.hasAddress(gateway) {
		log.Debugf("found existing host interface, returning")
		return hi, nil
	}

	//host interface is incomplete, try to rebuild it
	if hi.vxLink == nil {
		log.Debugf("%v interface nil, creating", hi.vxName)
		err := hi.createVxlanLink()
		if err != nil {
			return nil, err
		}
	}

	if hi.mvLink == nil {
		log.Debugf("%v interface nil, creating", hi.mvName)
		hmvl, err := hi.createMacvlanLink(hi.mvName)
		if err != nil {
			return nil, err
		}

		log.Debugf("initializing %v interface", hi.mvName)
		err = hi.initializeMacvlanLink(hmvl, hi.GetGateway(), netns.None(), "")
		if err != nil {
			return nil, err
		}

		hi.mvLink = hmvl
	}

	log.Debugf("validating/adding bypass route")
	err := hi.checkOrAddBypassRoute()
	if err != nil {
		return hi, err
	}

	log.Debugf("validating/adding bypass rule")
	err = hi.checkOrAddRule()
	if err != nil {
		return hi, err
	}

	if !hi.hasAddress(gateway) {
		log.Debugf("%v interface missing gateway address, adding", hi.mvName)
		return hi, netlink.AddrAdd(hi.mvLink, &netlink.Addr{IPNet: hi.GetGateway()})
	}

	return hi, nil
}

func (hi *HostInterface) checkOrAddRule() error {
	log.Debugf("checkOrAddRule()")
	net := iputil.NetworkID(hi.GetGateway())

	rules, err := netlink.RuleList(0)
	if err != nil {
		return err
	}

	for _, r := range rules {
		if iputil.SubnetEqualSubnet(r.Src, net) && iputil.SubnetEqualSubnet(r.Dst, net) && r.Table == DefaultVxlanRouteTable {
			log.Debugf("rule found, return")
			return nil
		}
	}

	log.Debugf("add rule")
	rule := netlink.NewRule()
	rule.Src = net
	rule.Dst = net
	rule.Table = DefaultVxlanRouteTable

	err = netlink.RuleAdd(rule)
	if err != nil {
		log.WithError(err).Errorf("failed to add rule")
		return err
	}

	return nil
}

func (hi *HostInterface) checkOrAddBypassRoute() error {
	log.Debugf("checkOrAddBypassRoute()")
	net := iputil.NetworkID(hi.GetGateway())

	routes, err := netlink.RouteListFiltered(0, &netlink.Route{Table: DefaultVxlanRouteTable}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return err
	}

	for _, r := range routes {
		if iputil.SubnetEqualSubnet(r.Dst, net) && r.LinkIndex == hi.mvLink.Attrs().Index {
			log.Debugf("bypass route found, return")
			return nil
		}
	}

	log.Debugf("add bypass route")
	err = netlink.RouteAdd(&netlink.Route{
		LinkIndex: hi.mvLink.Attrs().Index,
		Dst:       net,
		Table:     DefaultVxlanRouteTable,
	})
	if err != nil {
		log.WithError(err).Errorf("failed to add vxlan bypass route")
		return err
	}

	return nil
}

func (hi *HostInterface) hasAddress(addr *net.IPNet) bool {
	addrs, _ := netlink.AddrList(hi.mvLink, 0)

	for _, a := range addrs {
		if a.IP.Equal(addr.IP) && a.Mask.String() == addr.Mask.String() {
			return true
		}
	}

	return false
}

func (hi *HostInterface) createVxlanLink() error {
	nl := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name: hi.vxName,
			//MTU:  hi.VxlanParams.MTU,
		},
		VxlanId: hi.VxlanParams.ID,
	}

	if shwa, ok := hi.GetOption("vxlanhardwareaddr"); ok {
		hwa, _ := net.ParseMAC(shwa)
		netlink.LinkSetHardwareAddr(nl, hwa)
	}
	if qlen, ok := hi.GetOption("vxlantxqlen"); ok {
		nl.LinkAttrs.TxQLen, _ = strconv.Atoi(qlen)
	}
	if vtep, ok := hi.GetOption("vtepdev"); ok {
		nl.VtepDevIndex, _ = linkIndexByName(vtep)
	}
	if srcaddr, ok := hi.GetOption("srcaddr"); ok {
		nl.SrcAddr = net.ParseIP(srcaddr)
	}
	if group, ok := hi.GetOption("group"); ok {
		nl.Group = net.ParseIP(group)
	}
	if ttl, ok := hi.GetOption("ttl"); ok {
		nl.TTL, _ = strconv.Atoi(ttl)
	}
	if tos, ok := hi.GetOption("tos"); ok {
		nl.TOS, _ = strconv.Atoi(tos)
	}
	if learning, ok := hi.GetOption("learning"); ok {
		nl.Learning, _ = strconv.ParseBool(learning)
	}
	if proxy, ok := hi.GetOption("proxy"); ok {
		nl.Proxy, _ = strconv.ParseBool(proxy)
	}
	if rsc, ok := hi.GetOption("rsc"); ok {
		nl.RSC, _ = strconv.ParseBool(rsc)
	}
	if l2miss, ok := hi.GetOption("l2miss"); ok {
		nl.L2miss, _ = strconv.ParseBool(l2miss)
	}
	if l3miss, ok := hi.GetOption("l3miss"); ok {
		nl.L3miss, _ = strconv.ParseBool(l3miss)
	}
	if noage, ok := hi.GetOption("noage"); ok {
		nl.NoAge, _ = strconv.ParseBool(noage)
	}
	if gbp, ok := hi.GetOption("gbp"); ok {
		nl.GBP, _ = strconv.ParseBool(gbp)
	}
	if age, ok := hi.GetOption("age"); ok {
		nl.Age, _ = strconv.Atoi(age)
	}
	if limit, ok := hi.GetOption("limit"); ok {
		nl.Limit, _ = strconv.Atoi(limit)
	}
	if port, ok := hi.GetOption("port"); ok {
		nl.Port, _ = strconv.Atoi(port)
	}
	if pl, ok := hi.GetOption("portlow"); ok {
		nl.PortLow, _ = strconv.Atoi(pl)
	}
	if ph, ok := hi.GetOption("porthigh"); ok {
		nl.PortLow, _ = strconv.Atoi(ph)
	}

	err := netlink.LinkAdd(nl)
	if err != nil {
		return err
	}

	err = netlink.LinkSetUp(nl)
	if err != nil {
		return err
	}

	hi.vxLink = nl

	return nil
}

func (hi *HostInterface) createMacvlanLink(name string) (*netlink.Macvlan, error) {
	nl := &netlink.Macvlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        name,
			ParentIndex: hi.vxLink.Attrs().Index,
		},
		Mode: netlink.MACVLAN_MODE_BRIDGE,
	}

	var err error
	err = netlink.LinkAdd(nl)
	if err != nil {
		return nil, err
	}

	return nl, nil
}

func (hi *HostInterface) initializeMacvlanLink(nl *netlink.Macvlan, addr *net.IPNet, ns netns.NsHandle, ifname string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	rootns, err := netns.Get()
	if err != nil {
		return err
	}
	defer rootns.Close()

	if ns.IsOpen() {
		err = netlink.LinkSetNsFd(nl, int(ns))
		if err != nil {
			return err
		}

		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		netns.Set(ns)
		defer netns.Set(rootns)

		err = netlink.LinkSetName(nl, ifname)
		if err != nil {
			return err
		}
	}

	err = netlink.LinkSetUp(nl)
	if err != nil {
		return err
	}

	err = netlink.AddrAdd(nl, &netlink.Addr{IPNet: addr})
	if err != nil {
		return err
	}

	if ns.IsOpen() {
		// add default route through host to routing table in container namespace
		_, defaultDst, _ := net.ParseCIDR("0.0.0.0/0")
		err = netlink.RouteAdd(&netlink.Route{
			Dst: defaultDst,
			Gw:  hi.GetGateway().IP,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

//GetOption gets the names vxlan option from the options map
func (hi *HostInterface) GetOption(opt string) (string, bool) {
	val, ok := hi.VxlanParams.Options[opt]
	return val, ok
}

//GetGateway gets the gateway address and subnet from the vxlan config
func (hi *HostInterface) GetGateway() *net.IPNet {
	ipnet, _ := netlink.ParseIPNet(hi.VxlanParams.Cidr)
	return ipnet
}

//AddContainerLink adds a new macvlan link to the vxlan link, adds an IP, and puts it in the requested namespace.
func (hi *HostInterface) AddContainerLink(namespace, ifname string, addr *net.IPNet) (int, error) {
	cns, err := netns.GetFromPath(namespace)
	defer cns.Close()
	if err != nil {
		return -1, err
	}

	nsa := strings.Split(namespace, string(os.PathSeparator))
	if len(nsa) < 3 {
		return -1, fmt.Errorf("unexpected namespace path format")
	}

	//create interface with a temp name to prevent duplicates in the root namespace
	tempName := "cmvl_" + nsa[2]
	log.WithField("tempName", tempName).Debug("temporary interface name")
	cmvl, err := hi.createMacvlanLink(tempName)
	if err != nil {
		return -1, err
	}

	//set up, addr add, move to namespace
	err = hi.initializeMacvlanLink(cmvl, addr, cns, ifname)
	if err != nil {
		return -1, err
	}

	return cmvl.Index, nil
}

//DeleteContainerLink deletes the containers interface
func (hi *HostInterface) DeleteContainerLink(namespace, name string) error {
	rootns, err := netns.Get()
	if err != nil {
		return err
	}
	defer rootns.Close()

	cns, err := netns.GetFromPath(namespace)
	if err != nil {
		return err
	}
	defer cns.Close()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	err = netns.Set(cns)
	if err != nil {
		return err
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}

	err = netlink.LinkDel(link)
	if err != nil {
		return err
	}

	return netns.Set(rootns)
}

//Delete removes the components of the host interface from the host
func (hi *HostInterface) Delete() error {
	log.Debugf("HostInterface.Delete()")
	//TODO:
	//remove bypass rule
	//remove bypass route
	//delete vxlan (should cascade delete address and macvlan)
	return nil
}
