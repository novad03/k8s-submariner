<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->


- [Submariner](#submariner)
- [Architecture](#architecture)
  - [Network Path](#network-path)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
  - [Installation using subctl](#installation-using-subctl)
    - [Download](#download)
    - [Broker](#broker)
    - [Engine and route agent](#engine-and-route-agent)
  - [Installation using operator](#installation-using-operator)
  - [Manual installation using helm charts](#manual-installation-using-helm-charts)
    - [Setup](#setup)
    - [Broker Installation/Setup](#broker-installationsetup)
    - [Submariner Installation/Setup](#submariner-installationsetup)
      - [Installation of Submariner in each cluster](#installation-of-submariner-in-each-cluster)
  - [Validate Submariner is Working](#validate-submariner-is-working)
- [Building and Testing](#building-and-testing)
- [Known Issues/Notes](#known-issuesnotes)
  - [Openshift Notes](#openshift-notes)
- [Contributing](#contributing)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Submariner

[![End to End Tests](https://github.com/submariner-io/submariner/workflows/End%20to%20End%20Tests/badge.svg)](https://github.com/submariner-io/submariner/actions?query=workflow%3A%22End+to+End+Tests%22+branch%3Amaster)
[![Unit Tests](https://github.com/submariner-io/submariner/workflows/Unit%20Tests/badge.svg)](https://github.com/submariner-io/submariner/actions?query=workflow%3A%22Unit+Tests%22)
[![Linting](https://github.com/submariner-io/submariner/workflows/Linting/badge.svg)](https://github.com/submariner-io/submariner/actions?query=workflow%3ALinting+branch%3Amaster)
[![Release Images](https://github.com/submariner-io/submariner/workflows/Release%20Images/badge.svg)](https://github.com/submariner-io/submariner/actions?query=workflow%3A%22Release+Images%22)
[![Periodic](https://github.com/submariner-io/submariner/workflows/Periodic/badge.svg)](https://github.com/submariner-io/submariner/actions?query=workflow%3APeriodic+branch%3Amaster)

Submariner is a tool built to connect overlay networks of different Kubernetes clusters. While most testing is performed against Kubernetes
clusters that have enabled Flannel/Canal/Weavenet/OpenShiftSDN, Submariner should be compatible with any CNI-compatible cluster network
provider, as it utilizes off-the-shelf components such as strongSwan/Charon to establish IPsec tunnels between each Kubernetes cluster.

Note that Submariner is in the **pre-alpha** stage, and should not be used for production purposes. While we welcome usage/experimentation
with it, it is quite possible that you could run into severe bugs with it, and as such this is why it has this labeled status.

# Architecture

See the [Architecture docs on Submainer's website](https://submariner.io/architecture/).

## Network Path

The network path of Submariner varies depending on the origin/destination of the IP traffic. In all cases, traffic between two clusters will
transit between the leader elected (in each cluster) gateway nodes, through `ip xfrm` rules. Each gateway node has a running Charon daemon
which will perform IPsec keying and policy management.

When the source pod is on a worker node that is not the elected gateway node, the traffic destined for the remote cluster will transit
through the submariner VxLAN tunnel (`vx-submariner`) to the local cluster gateway node. On the gateway node, traffic is encapsulated in an
IPsec tunnel and forwarded to the remote cluster. Once the traffic reaches the destination gateway node, it is routed in one of two ways,
depending on the destination CIDR. If the destination CIDR is a pod network, the traffic is routed via CNI-programmed network. If the
destination CIDR is a service network, then traffic is routed through the facility configured via kube-proxy on the destination gateway
node.

# Prerequisites

See the [Prerequisites docs on Submainer's website](https://submariner.io/quickstart/#prerequisites).

# Installation

Submariner supports a number of different deployment models. An Operator is provided to manage Submariner deployments. The Operator can be
deployed via the `subctl` CLI helper utility, or via Helm charts, or directly. Submariner can also be deployed directly, without the
Operator, via Helm charts.

## Installation using Operator via subctl

Submairner provides the `subctl` CLI utility to simplify the deployment and maintenance of Submariner across your clusters.

See the [`subctl` docs on Submainer's website](https://submariner.io/deployment/subctl/).

## Manual installation using helm charts

### Setup

Submariner utilizes the following tools for installation:

- `kubectl`
- `helm`
- `base64`
- `cat`
- `tr`
- `fold`
- `head`

These instructions assume you have a combined kube config file with at least three contexts that correspond to the respective clusters.
Thus, you should be able to perform commands like

```shell
kubectl config use-context broker
kubectl config use-context west
kubectl config use-context east
```

Submariner utilizes Helm as a package management tool.

Before you start, you should add the `submariner-latest` chart repository to deploy the Submariner helm charts.

```shell
helm repo add submariner-latest https://submariner-io.github.io/submariner-charts/charts
```

### Broker Installation/Setup

The broker is the component that Submariner utilizes to exchange metadata information between clusters for connection information. This
should only be installed once on your central broker cluster. Currently, the broker is implemented by utilizing the Kubernetes API, but is
modular and will be enhanced in the future to bring support for other interfaces. The broker can be installed by using a helm chart.

First, you should switch into the context for the broker cluster

```shell
kubectl config use-context <BROKER_CONTEXT>
```

If you have not yet initialized Tiller on the cluster, you can do so with the following commands:

```shell
kubectl -n kube-system create serviceaccount tiller

kubectl create clusterrolebinding tiller \
  --clusterrole=cluster-admin \
  --serviceaccount=kube-system:tiller

helm init --service-account tiller
```

Wait for Tiller to initialize

```shell
kubectl -n kube-system  rollout status deploy/tiller-deploy
```

Once tiller is initialized, you can install the Submariner K8s Broker

```shell
helm repo update

SUBMARINER_BROKER_NS=submariner-k8s-broker

helm install submariner-latest/submariner-k8s-broker \
--name ${SUBMARINER_BROKER_NS} \
--namespace ${SUBMARINER_BROKER_NS}
```

Once you install the broker, you can retrieve the Kubernetes API server information (if not known) and service account token for the client
by utilizing the following commands:

<!-- markdownlint-disable line-length -->
```shell
SUBMARINER_BROKER_URL=$(kubectl -n default get endpoints kubernetes -o jsonpath="{.subsets[0].addresses[0].ip}:{.subsets[0].ports[?(@.name=='https')].port}")

SUBMARINER_BROKER_CA=$(kubectl -n ${SUBMARINER_BROKER_NS} get secrets -o jsonpath="{.items[?(@.metadata.annotations['kubernetes\.io/service-account\.name']=='${SUBMARINER_BROKER_NS}-client')].data['ca\.crt']}")

SUBMARINER_BROKER_TOKEN=$(kubectl -n ${SUBMARINER_BROKER_NS} get secrets -o jsonpath="{.items[?(@.metadata.annotations['kubernetes\.io/service-account\.name']=='${SUBMARINER_BROKER_NS}-client')].data.token}"|base64 --decode)
```
<!-- markdownlint-enable line-length -->

These environment variables will be utilized in later steps, so keep the values in a safe place.

### Submariner Installation/Setup

Submariner is installed by using a helm chart. Once you populate the environment variables for the token and broker URL, you should be able
to install Submariner into your clusters.

1. Generate a Pre-Shared Key for Submariner. This key will be used for all of your clusters, so keep it somewhere safe.

   ```shell
   SUBMARINER_PSK=$(cat /dev/urandom | LC_CTYPE=C tr -dc 'a-zA-Z0-9' | fold -w 64 | head -n 1)
   echo $SUBMARINER_PSK
   ```

1. Update the helm repository to pull the latest version of the Submariner charts

   ```shell
   helm repo update
   ```

#### Installation of Submariner in each cluster

Each cluster that will be connected must have Submariner installed within it. You must repeat these steps for each cluster that you add.

1. Set your kubeconfig context to your desired installation cluster

   ```shell
   kubectl config use-context <CLUSTER_CONTEXT>
   ```

1. Label your gateway nodes with the annotation `submariner.io/gateway=true`

   ```shell
   kubectl label node <DESIRED_NODE> "submariner.io/gateway=true"
   ```

1. Initialize Helm (if not yet done)

   ```shell
   kubectl -n kube-system create serviceaccount tiller

   kubectl create clusterrolebinding tiller \
     --clusterrole=cluster-admin \
     --serviceaccount=kube-system:tiller

   helm init --service-account tiller
   ```

1. Wait for Tiller to initialize

   ```shell
   kubectl -n kube-system  rollout status deploy/tiller-deploy
   ```

1. Install submariner into this cluster. The values within the following command correspond to the table below.

   ```shell
   helm install submariner-latest/submariner \
   --name submariner \
   --namespace submariner \
   --set ipsec.psk="${SUBMARINER_PSK}" \
   --set broker.server="${SUBMARINER_BROKER_URL}" \
   --set broker.token="${SUBMARINER_BROKER_TOKEN}" \
   --set broker.namespace="${SUBMARINER_BROKER_NS}" \
   --set broker.ca="${SUBMARINER_BROKER_CA}" \
   \
   --set submariner.clusterId="<CLUSTER_ID>" \
   --set submariner.clusterCidr="<CLUSTER_CIDR>" \
   --set submariner.serviceCidr="<SERVICE_CIDR>" \
   --set submariner.natEnabled="<NAT_ENABLED>"
   ```

   |Placeholder|Description|Default|Example|
   |:----------|:----------|:------|:------|
   |\<CLUSTER_ID>|Cluster ID (Must be RFC 1123 compliant)|""|west-cluster|
   |\<CLUSTER_CIDR>|Cluster CIDR for Cluster|""|`10.42.0.0/16`|
   |\<SERVICE_CIDR>|Service CIDR for Cluster|""|`10.43.0.0/16`|
<!-- markdownlint-disable line-length -->
   |\<NAT_ENABLED>|If in a cloud provider that uses 1:1 NAT between instances (for example, AWS VPC), you should set this to `true` so that Submariner is aware of the 1:1 NAT condition.|"false"|`false`|
<!-- markdownlint-enable line-length -->

## Validate Submariner is Working

Switch to the context of one of your clusters, i.e. `kubectl config use-context west`

Run an nginx container in this cluster, i.e. `kubectl run -n default nginx --image=nginx`

Retrieve the pod IP of the nginx container, looking under the "Pod IP" column for `kubectl get pod -n default`

Change contexts to your other workload cluster, i.e. `kubectl config use-context east`

Run a busybox pod and ping/curl the nginx pod:

```shell
kubectl run -i -t busybox --image=busybox --restart=Never
```

If you don't see a command prompt, try pressing enter.

```shell
ping <NGINX_POD_IP>
wget -O - <NGINX_POD_IP>
```

# Building and Testing

See the [Building and Testing docs on Submainer's website](https://submariner.io/contributing/building_testing/).

# Known Issues/Notes

## Openshift Notes

When running in Openshift, we need to grant the appropriate security context for the service accounts

   ```shell
   oc adm policy add-scc-to-user privileged system:serviceaccount:submariner:submariner-routeagent
   oc adm policy add-scc-to-user privileged system:serviceaccount:submariner:submariner-engine
   ```

# Contributing

See the [Contributing docs on Submainer's website](https://submariner.io/contributing/).
