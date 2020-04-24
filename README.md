# Warning
Though functional, this software is still in an alpha state. Though written with compatibility in mind, this software has not been tested with IPv6.

# Description
This CNI plugin is for container runtime operators who wish to use subnets as another administrative boundary for applications running on their cluster. When using this plugin, every cluster node becomes a router/gateway to any number of layer 2 virtual broadcast domains which are spanned across the entire cluster. This allows you to logically separate applications by network, regardless of which nodes the containers making up the application run on.

The network to which to connect (and optionally with a specific address) is passed in as part of the configuration. Optionally, the plugin can look in Kubernetes for pod annotations for the necessary information, since kubernetes does not yet support passing per pod annotations into a CNI plugin.

This plugin will utilize an external CNI IPAM plugin, but it requires that the IPAM plugin is aware of all addresses cluster-wide. If you utilize the corresponding [routetable-ipam](https://github.com/phdata/routetable-ipam) plugin, and a routing protocol, you can get efficient routing directly to a node running the destination container, without proxying through some other random node.

These distributed layer 2 networks are accomplished using a combination of the linux kernel's built in [vxlan](https://www.kernel.org/doc/Documentation/networking/vxlan.txt) and [macvlan](https://developers.redhat.com/blog/2018/10/22/introduction-to-linux-interfaces-for-virtual-networking/#macvlan) drivers. When a container is started, the plugin will create a macvlan interface bridged with the hosts macvlan interface, both as slave devices to the vxlan interface, and then move the new macvlan interface into the container namespace. The container's default route is set to the nodes macvlan address, and traffic originating to/from the container is routed through the node.

Caveats:
 * Every node in the cluster will require an address on the macvlan to route for containers that it hosts. In large clusters running IPv4, this could consume a lot of address space.
 * Currently requires all cluster nodes to participate in the same layer 2 network as the underlay. In theory this could be built to work on an NBMA, but some work would need to be done to accomplish that.
 * The host subnet routes create some interesting assymetric routing patterns that must be accounted for. Sometimes you can disable rp_filter. The plugin can optionally install a "bypass route" which sets up a custom rule to ensure that directly connected networks are routed out of the connected interface, instead of the more specific route being chosen.
 * If running in k8s, it is highly recommended that the DNS services be isolated on their own network. When pods communicate with the DNS service address, dns responses may not be un-natted by the kube-proxy iptables rules because there is a direct connection to the requesting container. This causes failures in DNS resolution.

Features:
 * Hosts will dynamically connect to a given vxlan, only when starting a container on that network (Dynamic disconnect when all containers on a vxlan are gone is still a TODO item).
 * You can specify a "default" network, where containers will be placed when the network is not specified.



The networking concepts and some of this code were inspired by and are originally from [here](https://github.com/TrilliumIT/vxrouter)
