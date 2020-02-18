# TF Serving Cache

TF Serving load balancer/model cache that dynamically loads and unloads models into TensorFlow Serving services on demand.

This is a work-in-progress.

![Image of architecture](https://raw.githubusercontent.com/mKaloer/TFServingCache/master/docs/img/architecture.png)

## Introduction

TF Serving is used to serve TensorFlow models in production. Usually, one TF Serving service runs a specific set of models, e.g. an inception model or maybe several custom models. All models are loaded into main memory, which limits the number of models to be served per TF Serving service. This approach works well when the number of models is small, and the cost of running a TF Serving service for model, or for a small set of models, is low.

However, in other situations it is required to serve a large number of models, e.g. when each tenant or user has its own specific model. The load on each model may not be high so the requirements to availability are fairly low, and it can be expensive to serve all models simultaneously with TF Serving due to the total memory requirements being high. For example, consider if 1000 users have a model of size 1gb each, the total memory requirement is 1 terabyte of memory if all models are to be served simultaneously.

## How It Works

TF Serving Cache distributes the models among a number of TensorFlow Serving services, each providing at most `N` models concurrently. The cache implements the TF Serving predict protocol (both REST and gRPC), and hence it does not require any modification to clients using TensorFlow Serving.

When a model is requested, TF Serving Cache will identify a TF Serving service that will serve the model. If the model is loaded and ready, the request will be forwarded to the TF Serving. Otherwise, the cache will fetch it from an external source (disk or AWS S3) and load it into TF Serving while unloading the least-recently-used model, before it forwards the request to TF Serving.<sup>[1](#credits)</sup>

In order to identify which TF Serving service that should provide a model, TF Serving Cache employs consistent hashing with a user-defined number of replicas per model. The number of TF Serving services available can be scaled dynamically, and either etcd or Consul are supported for service discovery.

## Todos

- REST (proxy):
  - Timeouts
  - Retries
- Smaller mutex-region in cache

## References

<a name="credits">1</a>: This Just-In-Time model loading is inspired by ideas by [@mpdn](https://github.com/mpdn)
