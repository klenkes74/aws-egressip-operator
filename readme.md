# AWS EgressIP Operator

This operator automates the assignment of egressIPs to namespaces. It is a fork of the [egressip-ipam-operator of the
GitHub Red Hat CoP](https://github.com/redhat-cop/egressip-ipam-operator) project.
Namespaces can opt in to receiving one or more egressIPs with the following annotation
`egressip-ipam-operator.redhat-cop.io/egressipam:<egressIPAM>` where egressIPAM is a basically ignored string. There
is no CR needed for this operator, it will check the AWS configuration instead.

When a namespace is created with the opt-in annotation: `egressip-ipam-operator.redhat-cop.io/egressipam=<egressIPAM>`,
AWS will assign a new IP to a selected instance this IP is assigned to the namespace.The `netnamespace` associated with
this namespace is updated to use that egressIP.

The use of these annotations will provide a compatible interface to the [egressip-ipam-operator of the GitHub Red Hat
CoP](https://github.com/redhat-cop/egressip-ipam-operator) in case you update to OCP4.


## Needed permissions for this operator
This operator needs some AWS permissions to do its work. These have to be handled via instance-profiles. The needed
permissions and the reasoning are:

AWS Permission | Reasoning
---------------|-----------------------------------
EC2:AssignPrivateIpAddresses | Manage the IP addresses of the instances.
EC2:DescribeInstances | Getting information about the instances (tags, networking interfaces).
EC2:DescribeNetworkInterfaces | Find instances by their IPv4 address(es).
EC2:DescribeSubnets | We need to read the subnets to find all CIDR of the account.
EC2:UnassignPrivateIpAddresses | Manage the IP addresses of the instances.


## Passing EgressIPs as input

The normal mode of operation of this operator is to pick a random IP from the configured CIDR. However, it also supports
a scenario where egressIPs are picked by an external process and passed as input.

In this case IPs must me passed as an annotation to the namespace, like this:
`egressip-ipam-operator.redhat-cop.io/egressips=IP1,IP2...`. The value of the annotation is a comma separated array of
ip with no spaces.

There is no check of the specified IPs. As long as the assignment via AWS works, everything is fine. It is the
responsibility of user to check if the IPs match the plans how to use them (e.g. one IP in every AWS availability zone).


## Assumptions

1. The AWS Subnets used for EgressIPs are tagged within AWS with kubernetes.io/cluster/<cluster-name>=<any value>
1. Nodes for getting IPs assigned are tagged within AWS with k8s.io/cluster-autoscaler/enabled=true
1. Nodes for getting IPs assigned are tagged within AWS with kubernetes.io/cluster/<cluster-name>=owned
1. Nodes for getting IPs assigned are tagged within AWS with ClusterNode=WorkerNode


## Deploying the Operator

This is a cluster-level operator that you can deploy in any namespace, `openshift-aws-egressip-operator` is recommended.
If you need to pin the operator to special nodes (like the OCP infranodes), please use the namespace node-selector
annotation to do that. May be helpful in restricting the AWS permissions to only a few nodes.

**Note:** *Create the namespace with `openshift.io/node-selector: ''` in order to deploy to master nodes. Or select the
 nodes you gave the needed AWS permissions.*

### Deploy with OpenShift Template

There is an OpenShift template in deploy/template.yaml that can be used for deploying the cluster. You need to create
the namespace before applying the template. There are some parameters to configure the deployment:

Template parameter   | Default Value | Description
---------------------|-------------|-----------------------
AWS_REGION           | eu-central-1                                | The amazon WS region the cluster operates in.
OPERATOR_IMAGE       | quay.io/klenkes74/aws-egressip-operator:dev | The Docker image to use.
OPERATOR_NAMESPACE   | openshift-aws-egress-operator               | The namespace for the operator to run in.
OPERATOR_NAME        | aws-egressip-operator                       | The name of the operator (only change if there are conflicts)
SERVICE_ACCOUNT_NAME | aws-egressip-operator                       | The name of the service account used (only change if there are conflicts)
ROLE_NAME            | aws-egressip-operator                       | The name of the role and clusterrole (only change if there are conflicts)

```shell script
git clone git@github.com:klenkes74/aws-egressip-operator.git ; cd aws-egressip-operator
oc process -f deploy/template.yaml -P AWS_REGION=<eu-central-2&gt; -P OPERATOR_NAMESPACE=<openshift-operators-aws-egressip&gt; | oc apply -f -
```

### Deploy with Helm

There is a Helm Chart in ./helm/aws-egressip-operator that can be used for deploying in the cluster. You need to create the namespace before applying the Helm Chart.

```shell script
git clone git@github.com:klenkes74/aws-egressip-operator.git ; cd aws-egressip-operator
helm upgrade aws-egressip-operator --namespace openshift-aws-egressip-operator ./helm/aws-egressip-operator --set clusterName=dbcs-hugo --install
```