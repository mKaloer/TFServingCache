# Motivation

In some use cases, the cache functionality looks usable even without scaling on more than one host, for example:
- I have shared folder (S3 bucket) with a couple of thousand models, and want to use only a couple of them in the local environment
- I want to test the 'caching' functionality of 'tfservingcache' and don't want to set up any discovery service

# How to try

Here's a small instruction on how to run TFservingCache with Docker:
- first of all, we need some ML models suitable for TFServing, the best place, I think, is TFServing repository, so
```bash
$ git clone https://github.com/tensorflow/serving
# Location of demo models
$ MODEL_REPO="$(pwd)/serving/tensorflow_serving/servables/tensorflow/testdata"
```
- then TFServing with the cache attached to it can be started like so:
```bash
$ cd <TFServingCache repostory folder>/deploy/docker-compose
$ docker-copose up --build
# the models will be consumed from MODEL_REPO assigned in the above step
# the --build flag will force build the cache docker image from sources
```
- and finally let try the predict API
```bash
$ curl http://localhost:8094/v1/models/saved_model_half_plus_two_cpu/versions/00000123
# Returns =>
# {
#  "model_version_status": [
#   {
#    "version": "123",
#    "state": "AVAILABLE",
#    "status": {
#     "error_code": "OK",
#     "error_message": ""
#    }
#   }
#  ]
# }

$ curl -d '{"instances": [1.0, 2.0, 5.0]}' -X POST http://localhost:8094/v1/models/saved_model_half_plus_two_cpu/versions/00000123:predict
# Returns => 
# { "predictions": [2.5, 3.0, 4.5] }
```

# TODO
- add tags for docker images
- check, is it possible to avoid hack with model.config
- write some kind of integration test for cache only 
- validate packed config.yaml 
