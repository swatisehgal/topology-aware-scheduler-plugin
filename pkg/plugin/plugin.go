package plugin

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	v1 "k8s.io/api/core/v1"
	v1qos "k8s.io/kubernetes/pkg/apis/core/v1/helper/qos"
	"k8s.io/apimachinery/pkg/api/resource"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	"k8s.io/kubernetes/pkg/scheduler/nodeinfo"

	topologyv1alpha1 "github.com/swatisehgal/topologyapi/pkg/apis/topology/v1alpha1"
	topoclientset "github.com/swatisehgal/topologyapi/pkg/generated/clientset/versioned"
	topoinformerexternal "github.com/swatisehgal/topologyapi/pkg/generated/informers/externalversions"
	topologyinformers "github.com/swatisehgal/topologyapi/pkg/generated/informers/externalversions"
	topoinformerv1alpha1 "github.com/swatisehgal/topologyapi/pkg/generated/informers/externalversions/topology/v1alpha1"
	bm "k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"
)

const (
	// TopologyAwareScheduler is the name of the plugin used in the plugin registry and configurations.
	Name                                = "TopologyAwareScheduler"
	namespace                           = "node-resource-topology"
	NoneTopologyManagerPolicy           = "none"
	BestEffortTopologyManagerPolicy     = "best-effort"
	RestrictedTopologyManagerPolicy     = "restricted"
	SingleNUMANodeTopologyManagerPolicy = "single-numa-node"
)

// Ensure we implement the FilterPluin interface
var _ framework.FilterPlugin = &TopologyAwareScheduler{}

// Ensure we implement the UnreservePlugin interface
var _ framework.UnreservePlugin = &TopologyAwareScheduler{}

type NodeTopologyMap map[string]topologyv1alpha1.NodeResourceTopology

type TopologyAwareScheduler struct {
	handle framework.FrameworkHandle
	// cli    *clientset.Clientset
	NodeTopologyInformer    topoinformerv1alpha1.NodeResourceTopologyInformer
	TopologyInformerFactory topoinformerexternal.SharedInformerFactory
	NodeTopologies          NodeTopologyMap
	NodeTopologyGuard       sync.RWMutex
}

// NewTopologyMatch initializes a new plugin and returns it.
func NewTopologyAwareScheduler(_ *runtime.Unknown, handle framework.FrameworkHandle) (framework.Plugin, error) {

	topologyAwareScheduler := &TopologyAwareScheduler{}
	topologyAwareScheduler.handle = handle

	clientConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("Please run from inside the cluster")
	}

	topoClient, err := topoclientset.NewForConfig(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("Can't create clientset for NodeTopologyResource: %s", err.Error())
	}

	topologyAwareScheduler.TopologyInformerFactory = topologyinformers.NewSharedInformerFactory(topoClient, 0)
	topologyAwareScheduler.NodeTopologyInformer = topologyAwareScheduler.TopologyInformerFactory.K8s().V1alpha1().NodeResourceTopologies()

	topologyAwareScheduler.NodeTopologyInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    topologyAwareScheduler.onTopologyCRDAdd,
			UpdateFunc: topologyAwareScheduler.onTopologyCRDUpdate,
			DeleteFunc: topologyAwareScheduler.onTopologyCRDFromDelete,
		},
	)

	go topologyAwareScheduler.NodeTopologyInformer.Informer().Run(context.Background().Done())
	topologyAwareScheduler.TopologyInformerFactory.Start(context.Background().Done())

	klog.Infof("start NodeTopologyInformer")

	topologyAwareScheduler.handle = handle
	topologyAwareScheduler.NodeTopologies = NodeTopologyMap{}

	return topologyAwareScheduler, nil
}

func (tm *TopologyAwareScheduler) Name() string {
	return Name
}

func filter(containers []v1.Container, nodes []topologyv1alpha1.NUMANodeResource, nodeName string, qos v1.PodQOSClass) *framework.Status {
	klog.Infof("nodeName: %s Calling filter with containers %v: %v", nodeName, spew.Sdump(containers), spew.Sdump(nodes))

	if qos == v1.PodQOSBestEffort {
		return nil
	}

	zeroQuantity := resource.MustParse("0")
	for _, container := range containers {
		bitmask := bm.NewEmptyBitMask()
		bitmask.Fill()
		for resource, quantity := range container.Resources.Requests {
			klog.Infof("nodeName: %s resource %v quantity :%v", nodeName, resource, quantity)

			resourceBitmask := bm.NewEmptyBitMask()
			for _, numaNode := range nodes {
				numaQuantity, ok := numaNode.Resources[resource]
				klog.Infof("nodeName: %s Numaqty %v requested qty :%v", nodeName, numaQuantity, quantity)

				// if can't find requested resource on the node - skip (don't set it as available NUMA node)
				// if unfound resource has 0 quantity probably this numa node can be considered
				if !ok && quantity.Cmp(zeroQuantity) != 0 {
					klog.Infof("nodeName: %v Going to continue: resource: %v quantity: %v", nodeName, resource, quantity)
					continue
				}
				// Check for the following:
				// 1. set numa node as possible node if resource is memory or Hugepages (until memory manager will not be merged and
				// memory will not be provided in CRD
				// 2. set numa node as possible node if resource is cpu and it's not guaranteed QoS, since cpu will flow
				// 3. set numa node as possible node if zero quantity for non existing resource was requested (TODO check topology manaager behaviour)
				// 4. otherwise check amount of resources
				if resource == v1.ResourceMemory ||
					strings.HasPrefix(string(resource), string(v1.ResourceHugePagesPrefix)) ||
					resource == v1.ResourceCPU && qos != v1.PodQOSGuaranteed ||
					quantity.Cmp(zeroQuantity) == 0 ||
					numaQuantity.Cmp(quantity) >= 0 {
					klog.Infof("nodeName: %s Adding numanode :%v resourceBitmask: %v", nodeName, numaNode.NUMAID, spew.Sdump(resourceBitmask))
					resourceBitmask.Add(numaNode.NUMAID)
					klog.Infof("nodeName: %s resourceBitmasks: %v", nodeName, spew.Sdump(bitmask))

				}
			}
			klog.Infof("nodeName: %s bitmask before AND is: %v", nodeName, bitmask)
			bitmask.And(resourceBitmask)
			klog.Infof("nodeName: %s After ANDing bitmask: %v", nodeName, spew.Sdump(bitmask))

		}
		if bitmask.IsEmpty() {
			// definitly we can't align container, so we can't align a pod
			return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("Can't align container: %s", container.Name))
		}
	}
	return nil
}


// checkTopologyPolicy return true if we're working with such policy
func checkTopologyPolicy(topologyPolicy string) bool {
	return len(topologyPolicy) > 0 && topologyPolicy == SingleNUMANodeTopologyManagerPolicy
}
func getTopologyPolicy(nodeTopologies NodeTopologyMap, nodeName string) string {
	if nodeTopology, ok := nodeTopologies[nodeName]; ok {
		return nodeTopology.TopologyPolicy
	}
	return ""
}

func (tm *TopologyAwareScheduler) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *nodeinfo.NodeInfo) *framework.Status {
	klog.Infof("TopologyAwareSchedulerPlugin: Filter was called for pod %s and node %+v", pod.Name, nodeInfo.Node().Name)
	klog.Infof("TopologyAwareSchedulerPlugin: Filter pod phase: %s", pod.Status.Phase)
	if nodeInfo.Node() == nil {
		return framework.NewStatus(framework.Error, fmt.Sprintf("Node is nil %s", nodeInfo.Node().Name))
	}
	nodeName := nodeInfo.Node().Name

	topologyPolicy := getTopologyPolicy(tm.NodeTopologies, nodeName)
	if !checkTopologyPolicy(topologyPolicy) {
		klog.Infof("Incorrect topology policy or topology policy is not specified: %s", topologyPolicy)
		return nil
	}
	containers := []v1.Container{}
	containers = append(pod.Spec.InitContainers, pod.Spec.Containers...)
	klog.Infof("Topology policy: %v", spew.Sdump(tm.NodeTopologies))
	tm.NodeTopologyGuard.RLock()
	defer tm.NodeTopologyGuard.RUnlock()
	return filter(containers, tm.NodeTopologies[nodeName].Nodes, nodeName, v1qos.GetPodQOS(pod))
}

func (tm *TopologyAwareScheduler) onTopologyCRDFromDelete(obj interface{}) {
	var nodeTopology *topologyv1alpha1.NodeResourceTopology
	switch t := obj.(type) {
	case *topologyv1alpha1.NodeResourceTopology:
		nodeTopology = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		nodeTopology, ok = t.Obj.(*topologyv1alpha1.NodeResourceTopology)
		if !ok {
			klog.Errorf("cannot convert to *v1alpha1.NodeResourceTopology: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("cannot convert to *v1alpha1.NodeResourceTopology: %v", t)
		return
	}

	klog.Infof("delete event for scheduled NodeResourceTopology %s/%s ",
		nodeTopology.Namespace, nodeTopology.Name)

	tm.NodeTopologyGuard.Lock()
	defer tm.NodeTopologyGuard.Unlock()
	if _, ok := tm.NodeTopologies[nodeTopology.Name]; ok {
		delete(tm.NodeTopologies, nodeTopology.Name)
	}
}

func (tm *TopologyAwareScheduler) onTopologyCRDUpdate(oldObj interface{}, newObj interface{}) {
	var nodeTopology *topologyv1alpha1.NodeResourceTopology
	switch t := newObj.(type) {
	case *topologyv1alpha1.NodeResourceTopology:
		nodeTopology = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		nodeTopology, ok = t.Obj.(*topologyv1alpha1.NodeResourceTopology)
		if !ok {
			klog.Errorf("cannot convert to *v1alpha1.NodeResourceTopology: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("cannot convert to *v1alpha1.NodeResourceTopology: %v", t)
		return
	}

	klog.V(5).Infof("update event for scheduled NodeResourceTopology %s/%s ",
		nodeTopology.Namespace, nodeTopology.Name)

	tm.NodeTopologyGuard.Lock()
	defer tm.NodeTopologyGuard.Unlock()
	tm.NodeTopologies[nodeTopology.Name] = *nodeTopology
}

func (tm *TopologyAwareScheduler) onTopologyCRDAdd(obj interface{}) {
	var nodeTopology *topologyv1alpha1.NodeResourceTopology
	switch t := obj.(type) {
	case *topologyv1alpha1.NodeResourceTopology:
		nodeTopology = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		nodeTopology, ok = t.Obj.(*topologyv1alpha1.NodeResourceTopology)
		if !ok {
			klog.Errorf("cannot convert to *v1alpha1.NodeResourceTopology: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("cannot convert to *v1alpha1.NodeResourceTopology: %v", t)
		return
	}
	klog.V(5).Infof("add event for scheduled NodeResourceTopology %s/%s ",
		nodeTopology.Namespace, nodeTopology.Name)

	tm.NodeTopologyGuard.Lock()
	defer tm.NodeTopologyGuard.Unlock()
	tm.NodeTopologies[nodeTopology.Name] = *nodeTopology
}

func (tm *TopologyAwareScheduler) Unreserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	klog.Infof("TopologyAwareScheduler: Unreserve was called for pod %s and node %+v", pod.Name, nodeName)
	klog.Infof("TopologyAwareScheduler: pod phase: %s", pod.Status.Phase)
}
