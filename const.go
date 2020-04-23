package vxlan

const (
	//DefaultIPAMTimeout is how long to wait for the IPAM plugin
	DefaultIPAMTimeout = 10

	//DefaultLockPath is the default path to store vxlan locks
	DefaultLockPath = "/tmp"

	//DefaultLockExt is the default extension of the lock file
	DefaultLockExt = ".lock"

	//DefaultVxlanRouteTable is the route table number used to store routes that override the /32 routes
	DefaultVxlanRouteTable = 192

	//NetworkAnnotation is the string key where we search for the name of the vxlan to join
	NetworkAnnotation = "go-libcni.phdata.io/NetworkName"

	//AddressAnnotation is the string key where we search for the IP address requested
	AddressAnnotation = "go-libcni.phdata.io/RequestedAddress"
)
