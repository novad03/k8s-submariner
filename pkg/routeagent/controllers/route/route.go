package route

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	v1 "github.com/submariner-io/submariner/pkg/apis/submariner.io/v1"
	clientset "github.com/submariner-io/submariner/pkg/client/clientset/versioned"
	informers "github.com/submariner-io/submariner/pkg/client/informers/externalversions/submariner.io/v1"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	k8sv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	podinformer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

type Controller struct {
	clusterID       string
	objectNamespace string

	submarinerClientSet    clientset.Interface
	clientSet              *kubernetes.Clientset
	clustersSynced         cache.InformerSynced
	endpointsSynced        cache.InformerSynced
	smRouteAgentPodsSynced cache.InformerSynced

	clusterWorkqueue  workqueue.RateLimitingInterface
	endpointWorkqueue workqueue.RateLimitingInterface
	podWorkqueue      workqueue.RateLimitingInterface

	gatewayNodeIP    net.IP
	localClusterCidr []string
	localServiceCidr []string
	remoteSubnets    []string

	vxlanDevice *vxLanIface
	vxlanGw     net.IP
	remoteVTEPs []string

	isGatewayNode bool
	link          *net.Interface
}

const VXLAN_IFACE = "vxlan100"
const VXLAN_PORT = "4800"
const VXLAN_VTEP_NETWORK_PREFIX = "240"
const SM_POSTROUTING_CHAIN = "SUBMARINER-POSTROUTING"
const SM_ROUTE_AGENT_FILTER = "app=submariner-routeagent"

func NewController(clusterID string, ClusterCidr []string, ServiceCidr []string, objectNamespace string, link *net.Interface, submarinerClientSet clientset.Interface, clientSet *kubernetes.Clientset, clusterInformer informers.ClusterInformer, endpointInformer informers.EndpointInformer, podInformer podinformer.PodInformer) *Controller {
	controller := Controller{
		clusterID:              clusterID,
		objectNamespace:        objectNamespace,
		localClusterCidr:       ClusterCidr,
		localServiceCidr:       ServiceCidr,
		submarinerClientSet:    submarinerClientSet,
		clientSet:              clientSet,
		link:                   link,
		isGatewayNode:          false,
		clustersSynced:         clusterInformer.Informer().HasSynced,
		endpointsSynced:        endpointInformer.Informer().HasSynced,
		smRouteAgentPodsSynced: podInformer.Informer().HasSynced,
		clusterWorkqueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Clusters"),
		endpointWorkqueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Endpoints"),
		podWorkqueue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Pods"),
	}

	clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueCluster,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueCluster(new)
		},
		DeleteFunc: controller.handleRemovedCluster,
	})

	endpointInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueEndpoint,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueEndpoint(new)
		},
		DeleteFunc: controller.handleRemovedEndpoint,
	})

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueuePod,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueuePod(new)
		},
		DeleteFunc: controller.handleRemovedPod,
	})

	return &controller
}

func (r *Controller) Run(stopCh <-chan struct{}) error {
	var wg sync.WaitGroup
	wg.Add(1)
	defer utilruntime.HandleCrash()

	// Start the informer factories to begin populating the informer caches
	klog.V(4).Infof("Starting Route Controller. ClusterID: %s, localClusterCIDR: %v, localServiceCIDR: %v", r.clusterID, r.localClusterCidr, r.localServiceCidr)

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for endpoint informer caches to sync.")
	if ok := cache.WaitForCacheSync(stopCh, r.endpointsSynced, r.clustersSynced, r.smRouteAgentPodsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	// Create the necessary IPTable chains in the filter and nat tables.
	err := r.createIPTableChains()
	if err != nil {
		return fmt.Errorf("Failed to program the necessary iptable rules.")
	}

	// let's go ahead and pre-populate clusters
	clusters, err := r.submarinerClientSet.SubmarinerV1().Clusters(r.objectNamespace).List(metav1.ListOptions{})
	if err != nil {
		klog.Fatalf("error while retrieving all clusters: %v", err)
	}

	// Program iptables rules for traffic destined to all the remote cluster CIDRs
	for _, cluster := range clusters.Items {
		if cluster.Spec.ClusterID != r.clusterID {
			r.updateIptableRulesForInterclusterTraffic(append(cluster.Spec.ClusterCIDR, cluster.Spec.ServiceCIDR...))
		}
	}

	// Query all the submariner-route-agent daemonSet PODs running in the local cluster.
	podList, err := r.clientSet.CoreV1().Pods(r.objectNamespace).List(metav1.ListOptions{LabelSelector: SM_ROUTE_AGENT_FILTER})
	if err != nil {
		klog.Fatalf("error while retrieving all submariner-route-agent pods: %v", err)
	}

	for index, pod := range podList.Items {
		klog.V(4).Infof("In %s, podIP of submariner-route-agent[%d] is %s", r.clusterID, index, pod.Status.PodIP)
		r.populateRemoteVtepIps(pod.Status.PodIP)
	}

	klog.Info("Starting workers")
	go wait.Until(r.runClusterWorker, time.Second, stopCh)
	go wait.Until(r.runEndpointWorker, time.Second, stopCh)
	go wait.Until(r.runPodWorker, time.Second, stopCh)
	wg.Wait()
	<-stopCh
	klog.Info("Shutting down workers")
	return nil
}

func (r *Controller) runClusterWorker() {
	for r.processNextCluster() {

	}
}

func (r *Controller) runEndpointWorker() {
	for r.processNextEndpoint() {

	}
}

func (r *Controller) runPodWorker() {
	for r.processNextPod() {

	}
}

func (r *Controller) updateIptableRulesForInterclusterTraffic(inputCidrBlocks []string) {
	for _, remoteCidrBlock := range inputCidrBlocks {
		if !containsString(r.remoteSubnets, remoteCidrBlock) {
			r.remoteSubnets = append(r.remoteSubnets, remoteCidrBlock)
			r.programIptableRulesForInterClusterTraffic(remoteCidrBlock)
		}
	}
}

func (r *Controller) populateRemoteVtepIps(vtepIP string) {
	if !containsString(r.remoteVTEPs, vtepIP) {
		r.remoteVTEPs = append(r.remoteVTEPs, vtepIP)
	}
}

func (r *Controller) deleteRemoteVtepIp(vtepIP string) {
	if containsString(r.remoteVTEPs, vtepIP) {
		r.remoteVTEPs = r.deleteVepEntry(r.remoteVTEPs, vtepIP)
	}
}

func (r *Controller) deleteVepEntry(vtepList []string, entryToDelete string) []string {
	for i, v := range vtepList {
		if v == entryToDelete {
			return append(vtepList[:i], vtepList[i+1:]...)
		}
	}
	return vtepList
}

func (r *Controller) processNextCluster() bool {
	obj, shutdown := r.clusterWorkqueue.Get()
	if shutdown {
		return false
	}
	err := func() error {
		defer r.clusterWorkqueue.Done(obj)
		klog.V(4).Infof("Processing cluster object: %v", obj)
		key := obj.(string)
		ns, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return fmt.Errorf("Error while splitting meta namespace key %s: %v", key, err)
		}
		cluster, err := r.submarinerClientSet.SubmarinerV1().Clusters(ns).Get(name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("Error retrieving submariner cluster object %s: %v", name, err)
		}

		if cluster.Spec.ClusterID == r.clusterID {
			klog.V(6).Infof("cluster ID matched the cluster ID of this cluster, not adding it to the cidr list")
			return nil
			// no need to reconcile because this endpoint isn't ours
		}

		r.updateIptableRulesForInterclusterTraffic(append(cluster.Spec.ClusterCIDR, cluster.Spec.ServiceCIDR...))

		r.clusterWorkqueue.Forget(obj)
		klog.V(4).Infof("cluster processed by route controller")
		return nil
	}()

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (r *Controller) getVxlanVtepIPAddress() (net.IP, *net.IPNet, error) {
	ipAddr, ipNetwork, err := r.getHostIfaceIPAddress()
	if err != nil {
		klog.Errorf("Unable to retrieve the IPv4 address on the Host %v", err)
		return nil, nil, err
	}

	ipSlice := strings.Split(ipAddr.String(), ".")
	ipSlice[0] = VXLAN_VTEP_NETWORK_PREFIX
	vxlanIP := net.ParseIP(strings.Join(ipSlice, "."))
	return vxlanIP, ipNetwork, nil
}

func (r *Controller) getHostIfaceIPAddress() (net.IP, *net.IPNet, error) {
	addrs, err := r.link.Addrs()
	if len(addrs) > 0 {
		for i := range addrs {
			ipAddr, ipNetwork, err := net.ParseCIDR(addrs[i].String())
			if err != nil {
				klog.Errorf("Unable to ParseCIDR : %v\n", addrs)
			}
			if ipAddr.To4() != nil {
				return ipAddr, ipNetwork, nil
			}
		}
	}
	return nil, nil, err
}

func (r *Controller) createVxLANInterface(isGatewayDevice bool) error {
	vtepIP, vtepMask, err := r.getVxlanVtepIPAddress()
	if err != nil {
		klog.Fatalf("Failed to derive the vxlan vtepIP on the Gateway Node %v", err)
	}

	if isGatewayDevice {
		vtepPort, _ := strconv.Atoi(VXLAN_PORT)
		attrs := &vxLanAttributes{
			name:     VXLAN_IFACE,
			vxlanId:  100,
			group:    nil,
			srcAddr:  nil,
			vtepPort: vtepPort,
			mtu:      1450,
		}

		r.vxlanDevice, err = newVxlanIface(attrs)
		if err != nil {
			klog.Fatalf("Failed to create vxlan interface on Gateway Node: %v", err)
		}

		for _, fdbAddress := range r.remoteVTEPs {
			err = r.vxlanDevice.AddFDB(net.ParseIP(fdbAddress), "00:00:00:00:00:00")
			if err != nil {
				klog.Fatalf("Failed to add FDB entry on the Gateway Node vxlan iface %v", err)
			}
		}

		// Enable loose mode (rp_filter=2) reverse path filtering on the vxlan interface.
		err = ioutil.WriteFile("/proc/sys/net/ipv4/conf/"+VXLAN_IFACE+"/rp_filter", []byte("2"), 0644)
		if err != nil {
			klog.Errorf("Unable to update proc entry, err: %s", err)
		} else {
			klog.Errorf("Successfully updated proc entry ")
		}

	} else {
		// non-Gateway/Worker Node
		vtepPort, _ := strconv.Atoi(VXLAN_PORT)
		attrs := &vxLanAttributes{
			name:     VXLAN_IFACE,
			vxlanId:  100,
			group:    r.gatewayNodeIP,
			srcAddr:  vtepIP,
			vtepPort: vtepPort,
			mtu:      1450,
		}

		r.vxlanDevice, err = newVxlanIface(attrs)
		if err != nil {
			klog.Fatalf("Failed to create vxlan interface on non-Gateway Node: %v", err)
		}
	}

	err = r.vxlanDevice.configureIPAddress(vtepIP, vtepMask.Mask)
	if err != nil {
		klog.Fatalf("Failed to configure vxlan interface ipaddress on the Gateway Node %v", err)
	}

	return nil
}

func (r *Controller) processNextPod() bool {
	pod, shutdown := r.podWorkqueue.Get()
	if shutdown {
		return false
	}
	err := func() error {
		defer r.podWorkqueue.Done(pod)
		pod := pod.(*k8sv1.Pod)

		klog.V(4).Infof("In processNextPod, POD HostIP is %s", pod.Status.HostIP)
		r.populateRemoteVtepIps(pod.Status.PodIP)

		// A new Node (identified via a Submariner-route-agent daemonset pod event) is added to the cluster
		// On the GatewayDevice, update the vxlan fdb entry (i.e., remote Vtep) for the newly added node.
		if r.isGatewayNode {
			ret := r.vxlanDevice.AddFDB(net.ParseIP(pod.Status.PodIP), "00:00:00:00:00:00")
			if ret != nil {
				klog.Errorf("Failed to add FDB entry on the Gateway Node vxlan iface %v", ret)
			}
		}

		r.podWorkqueue.Forget(pod)
		klog.V(4).Infof("Pod event processed by route controller")
		return nil
	}()

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (r *Controller) processNextEndpoint() bool {
	obj, shutdown := r.endpointWorkqueue.Get()
	if shutdown {
		return false
	}
	err := func() error {
		defer r.endpointWorkqueue.Done(obj)
		klog.V(4).Infof("Handling object in handleEndpoint")
		klog.V(4).Infof("Processing endpoint object: %v", obj)
		key := obj.(string)
		ns, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return fmt.Errorf("Error while splitting meta namespace key %s: %v", key, err)
		}
		endpoint, err := r.submarinerClientSet.SubmarinerV1().Endpoints(ns).Get(name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("Error retrieving submariner endpoint object %s: %v", name, err)
		}

		if endpoint.Spec.ClusterID != r.clusterID {
			klog.V(6).Infof("Endpoint didn't match the cluster ID of this cluster")
			return nil
			// no need to reconcile because this endpoint isn't ours
		}

		hostname, err := os.Hostname()
		if err != nil {
			klog.Fatalf("unable to determine hostname: %v", err)
		}

		klog.V(6).Infof("Local Cluster Gateway Node IP is %s", endpoint.Spec.PrivateIP)
		r.gatewayNodeIP = net.ParseIP(endpoint.Spec.PrivateIP)

		ipSlice := strings.Split(r.gatewayNodeIP.String(), ".")
		ipSlice[0] = VXLAN_VTEP_NETWORK_PREFIX
		// remoteVtepIP is used while programming the routing rules
		remoteVtepIP := net.ParseIP(strings.Join(ipSlice, "."))
		r.vxlanGw = remoteVtepIP

		if endpoint.Spec.Hostname == hostname {
			r.cleanRoutes()
			r.isGatewayNode = true
			if r.createVxLANInterface(true) != nil {
				klog.Fatalf("Unable to create VxLAN interface on GatewayNode (%s): %v", hostname, err)
			}
			klog.V(6).Infof("not reconciling routes because we appear to be the gateway host")
			return nil
		}

		r.isGatewayNode = false
		if r.createVxLANInterface(false) != nil {
			klog.Fatalf("Unable to create VxLAN interface on non-GatewayNode (%s): %v", endpoint.Spec.Hostname, err)
		}

		r.cleanXfrmPolicies()
		err = r.reconcileRoutes()
		if err != nil {
			r.endpointWorkqueue.AddRateLimited(obj)
			return fmt.Errorf("Error while reconciling routes %v", err)
		}

		r.endpointWorkqueue.Forget(obj)
		klog.V(4).Infof("endpoint processed by route controller")
		return nil
	}()

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (r *Controller) enqueueCluster(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	r.clusterWorkqueue.AddRateLimited(key)
}

func (r *Controller) enqueueEndpoint(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	r.endpointWorkqueue.AddRateLimited(key)
}

func (r *Controller) enqueuePod(obj interface{}) {
	klog.V(6).Infof("Enqueueing pod for route controller %v", obj)
	pod := obj.(*k8sv1.Pod)

	// Add the POD event to the workqueue only if the sm-route-agent podIP does not exist in the local cache.
	if !containsString(r.remoteVTEPs, pod.Status.HostIP) {
		r.podWorkqueue.AddRateLimited(obj)
	}
}

func (r *Controller) handleRemovedEndpoint(obj interface{}) {
	// ideally we should attempt to remove all routes if the endpoint matches our cluster ID
	var object *v1.Endpoint
	var ok bool
	klog.V(4).Infof("Handling object in handleEndpoint")
	if object, ok = obj.(*v1.Endpoint); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not convert object %v to an Endpoint", obj)
			return
		}
		object, ok = tombstone.Obj.(*v1.Endpoint)
		if !ok {
			klog.Errorf("Could not convert object tombstone %v to an Endpoint", tombstone.Obj)
			return
		}
		klog.V(4).Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}
	klog.V(4).Infof("Informed of removed endpoint for route controller object: %v", object)
	hostname, err := os.Hostname()
	if err != nil {
		klog.Fatalf("Could not retrieve hostname: %v", err)
	}
	if object.Spec.Hostname == hostname {
		r.cleanRoutes()
	}
	klog.V(4).Infof("Removed routes from host")
}

func (r *Controller) handleRemovedCluster(obj interface{}) {
	// ideally we should attempt to remove all routes if the endpoint matches our cluster ID
}

func (r *Controller) handleRemovedPod(obj interface{}) {
	klog.V(6).Infof("Removing podIP in route controller %v", obj)
	pod := obj.(*k8sv1.Pod)

	if containsString(r.remoteVTEPs, pod.Status.HostIP) {
		r.deleteRemoteVtepIp(pod.Status.HostIP)
		if r.isGatewayNode {
			ret := r.vxlanDevice.DelFDB(net.ParseIP(pod.Status.PodIP), "00:00:00:00:00:00")
			if ret != nil {
				klog.Errorf("Failed to delete FDB entry on the Gateway Node vxlan iface %v", ret)
			}
		}
	}
}

func (r *Controller) cleanRoutes() {
	link, err := netlink.LinkByName(VXLAN_IFACE)
	if err != nil {
		klog.Errorf("Error retrieving link by name %s: %v", VXLAN_IFACE, err)
		return
	}
	currentRouteList, err := netlink.RouteList(link, syscall.AF_INET)
	if err != nil {
		klog.Errorf("Error retrieving routes on the link %s: %v", VXLAN_IFACE, err)
		return
	}
	for _, route := range currentRouteList {
		klog.V(6).Infof("Processing route %v", route)
		if route.Dst == nil || route.Gw == nil {
			klog.V(6).Infof("Found nil gw or dst")
		} else {
			if containsString(r.remoteSubnets, route.Dst.String()) {
				klog.V(6).Infof("Removing route %s", route.String())
				if err = netlink.RouteDel(&route); err != nil {
					klog.Errorf("Error removing route %s: %v", route.String(), err)
				}
			}
		}
	}
}

func (r *Controller) cleanXfrmPolicies() {

	currentXfrmPolicyList, err := netlink.XfrmPolicyList(syscall.AF_INET)

	if err != nil {
		klog.Errorf("Error retrieving current xfrm policies: %v", err)
		return
	}

	for _, xfrmPolicy := range currentXfrmPolicyList {
		klog.V(6).Infof("Deleting XFRM policy %s", xfrmPolicy.String())
		if err = netlink.XfrmPolicyDel(&xfrmPolicy); err != nil {
			klog.Errorf("Error Deleting XFRM policy %s: %v", xfrmPolicy.String(), err)
		}
	}
}

// Reconcile the routes installed on this device using rtnetlink
func (r *Controller) reconcileRoutes() error {
	link, err := netlink.LinkByName(VXLAN_IFACE)
	if err != nil {
		return fmt.Errorf("Error retrieving link by name %s: %v", VXLAN_IFACE, err)
	}

	currentRouteList, err := netlink.RouteList(link, syscall.AF_INET)

	if err != nil {
		return fmt.Errorf("Error retrieving routes for link %s: %v", VXLAN_IFACE, err)
	}

	// First lets delete all of the routes that don't match
	for _, route := range currentRouteList {
		// contains(endpoint destinations, route destination string, and the route gateway is our actual destination
		klog.V(6).Infof("Processing route %v", route)
		if route.Dst == nil || route.Gw == nil {
			klog.V(6).Infof("Found nil gw or dst")
		} else {
			if containsString(r.remoteSubnets, route.Dst.String()) && route.Gw.Equal(r.vxlanGw) {
				klog.V(6).Infof("Found route %s with gw %s already installed", route.String(), route.Gw.String())
			} else {
				klog.V(6).Infof("Removing route %s", route.String())
				if err = netlink.RouteDel(&route); err != nil {
					klog.Errorf("Error removing route %s: %v", route.String(), err)
				}
			}
		}
	}

	currentRouteList, err = netlink.RouteList(link, syscall.AF_INET)

	if err != nil {
		return fmt.Errorf("Error retrieving routes for link %s: %v", VXLAN_IFACE, err)
	}

	// let's now add the routes that are missing
	for _, cidrBlock := range r.remoteSubnets {
		_, dst, err := net.ParseCIDR(cidrBlock)
		if err != nil {
			klog.Errorf("Error parsing cidr block %s: %v", cidrBlock, err)
			break
		}
		route := netlink.Route{
			Dst:       dst,
			Gw:        r.vxlanGw,
			Scope:     unix.RT_SCOPE_UNIVERSE,
			LinkIndex: link.Attrs().Index,
			Protocol:  4,
		}
		found := false
		for _, curRoute := range currentRouteList {
			if curRoute.Gw == nil || curRoute.Dst == nil {

			} else {
				if curRoute.Gw.Equal(route.Gw) && curRoute.Dst.String() == route.Dst.String() {
					klog.V(6).Infof("Found equivalent route, not adding")
					found = true
				}
			}
		}

		if !found {
			err = netlink.RouteAdd(&route)
			if err != nil {
				klog.Errorf("Error adding route %s: %v", route.String(), err)
			}
		}
	}
	return nil
}

func containsString(c []string, s string) bool {
	for _, v := range c {
		if v == s {
			return true
		}
	}
	return false
}
