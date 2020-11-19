# Topology-aware Scheduler plugin

An out-of-tree scheduling plugin to enable Topology-aware scheduling in kubernetes

As part of the enablement of Topology-aware scheduling in kubernetes the scheduler plugin has been proposed as in-tree.

KEP:[Simplified version of topology manager in kube-scheduler #1858](https://github.com/kubernetes/enhancements/pull/1858)

PR: https://github.com/kubernetes/kubernetes/pull/90708

To enable faster development velocity, testing and community adoption we are also providing it as an out of tree scheduler plugin. This out-of-tree implementation is based on the PR and KEP mentioned above.

This scheduler plugin uses [sample-device-plugin](https://github.com/swatisehgal/sample-device-plugin) for simulating devices on various numa nodes.

## Installation

To deploy the scheduler plugin run:

```bash
make deploy
```

To deploy a test pod to be deployed by scheduler plugin run:

```bash
make deploy-pod
```
