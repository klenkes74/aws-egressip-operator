# How does the operator operate?

## Handle Resource: Namespace
1. Check if the namespace has the annotations assigned
2. If the modification timestamp is not set -> Assign IP address to namespace
3. If the modification timestamp is set -> do nothing
4. If the annotations have been removed -> Unassign IP address from namespace

## Handle Resource NetNamespace
1. If the IP got removed -> Verify against namespace
2. If namespace still got the IP, reforce the IP into NetNamespace
3. If namespace has no IP (or other IPs set) do the change

## Handle Resource: Node
1. Node is new: do nothing (there are no IPs yet)
2. Node is deleted: redistribute the IPs on other nodes.
3. Node is updated: Check all IPs (verify AWS setup matching the node configuration)

## Handle Resource: HostSubnet
1. Verify IP -> AWS

## Flow: Assign IP address to namespace
1. Get all compute nodes in cluster (harvest data about distribution of IPs to nodes)
2. Select the compute node with least IPs in every availability zone (random if there are multiple)
3. Attach a new IP to the primary interface of one node per availability zone via AWS and retrieve the IPs
4. Put the IPs into OpenShift EgressIP
5. Document IPs in annotation "egressip-ipam-operator.redhat-cop.io/egressips"
6. Document timestamp in "egressip-ipam-operator.redhat-cop.io/modified"

## Flow: Assign a specified IP address to namespace
1. Get all compute nodes in cluster
2. Select the compute node with least IPs in the AZ of the specified IP
3. Attach the specified IP to the primary interface of the node. (repeat for all IPs in the different AZs)
4. If not successful: record failure status (must be "IP not available, already used")
5. Put the IPs into OpenShift EgressIP
6. Document IPs in annotation "egressip-ipam-operator.redhat-cop.io/egressips"
7. Document timestamp in "egressip-ipam-operator.redhat-cop.io/modified"

## Flow: Unassign IP address from namespace
1. Get IPs from "egressip-ipam-operator.redhat-cop.io/egressips"
2. Get NetNamespace and HostSubnet for all IPs
3. Remove IPs from HostSubnets
4. Remove IPs from AWS
5. Remove annotations from namespace

## Flow: Verify IP -> AWS
1. Get HostSubnet with IP addresses
2. Get AWS Network interfaces with IP addresses
3. Diff AWS and OCP IP interfaces.
4. If AWS is missing -> Configure AWS

# Troubleshooting
In case a problem with IPs and a namespace exist, the metric egress_handle_failure{namespace=<namespace>} will get a 
positive integer value (it's a gauge counting the failures). As soon as the problem is solved, the gauge will be set to
zero.

So you know the namespace which has problems. Now the following checks have to be done:

## Checklist for an EgressIP problem
1. Are the annotations still valid on the namespace?

   If they are missing - re-add them.

1. Are the annotations still valid on the netnamespace?

   If they are missing - re-add them

1. Are the EgressIPs set on the netnamespace?

   If they are missing - re-add them

1. Are the hostsubnets configured with all ips (jsonpath .egressIPs)?

   If they are missing - re-add them

1. Do the hosts in AWS have the IPs as secondary IPs on the interfache eth0?

   If they are missing - re-add them (you could use the web UI)