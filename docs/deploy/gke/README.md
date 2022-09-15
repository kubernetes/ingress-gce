# Overview

Welcome, you are reading this because you are interested in running a **self-managed** version of the
Ingress-GCE Controller on GKE!

Note that on GKE, Ingress-GCE runs by default but it is managed for you by GKE. This document
will teach you how to release GKE from managing GLBC and put that power in your hands.

**Disclaimer: Running this script could potentially be disruptive to traffic
so if you want to run this on a production cluster, do so at your own risk.
Furthermore you should refrain from contacting GKE support if there are issues.**

# Important Prerequisites

There are two prerequisite steps that need to be taken before running the script.

Run the command below:

`gcloud config list`

You will need to ensure a couple things. First, ensure that the project listed
under [core]/project is the one that contains your cluster. Second, ensure that
the current logged-in account listed under [core]/account is the owner of the project.
In other words, this account should have full project ownership permissions and be listed as
having the "Owner" role on the IAM page of your GCP project. You might ask why this
is needed? Well, the reason is that our [script](gke-self-managed.sh) invokes a kubectl command which
creates a new k8s RBAC role (see below for explanation why). In order to do this, the
current user must be the project owner. The "Owner" role also gives the account
permission to do basically anything so all commands the script runs should
theoretically work. If not, the script will do its best to fail gracefully
and let you know what might have went wrong.

# Important Details

Most likely, you want to know what this script is doing to your cluster in order
to run the new controller and why it is doing it. If you do not, care then you
can go ahead and skip this section.

Here is a brief summary of each major thing we do and why:

1. (If applicable) figure out information you didn't provide from the cluster and
   build and push the GLBC image.
2. Create a new k8s RBAC role
    * On the master, the GLBC has unauthenticated access to the k8s API server.
      Once we move the GLBC to the cluster, that path is gone. Therefore, we need to
      configure a new RBAC role that allows GLBC the same access.
3. Create new GCP service account + key
    * On the master, the GLBC is authorized to use the GCP Compute API through a
      token pulled from a private GKE endpoint. Moving to the cluster will result in
      us not being able to utilize this. Therefore, we need to create a new GCP
      service account and a corresponding key which will grant access.
4. Update the cluster to turn off the default GLBC
    * This restarts the cluster master. The API server will be temporarily unavailable.
5. Create our own default backend and custom GLBC on the cluster
    * We need to startup a new default backend because the mechanism we
      use to turn off the master GLBC removes both the GLBC and the default backend.
    * This is dependent on the default GLBC being running initially, as we use
      the same nodeport.
    * Because we have to recreate the default backend, there will be a small
      segment of time when requests to the default backend will time out.

The script is commented heavily, so it should be pretty easy to follow along
with what we described above.

## Dependencies

Here is an explanation of each script dependency.

1. [gce.conf](../resources/gce.conf)
    * This file normally sits on the GKE master and provides important config for
      the GCP Compute API client within the GLBC. The GLBC is configured to know
      where to look for this file. In this case, we simply mount a customized copy
      of the file as a volume and tell GLBC to look for it there.
2. [default-http-backend.yaml](../resources/default-http-backend.yaml)
    * This file contains the specifications for both the default-http-backend
      deployment and service. This is no different than what you are used to
      seeing in your cluster. In this case, we need to recreate the default
      backend since turning off the GLBC on the master removes it. Note that we
      modify the file to use the same node port as before we create the resource.
3. [rbac.yaml](../resources/rbac.yaml)
    * This file contains specification for an RBAC role which gives the GLBC
      access to the resources it needs from the k8s API server.
4. [glbc.yaml](../resources/glbc.yaml)
    * This file contains the specification for the GLBC deployment. Notice that in
      this case, we need a deployment because we want to preserve the controller
      in case of node restarts. The resource we create is from a customized copy of
      this file that uses the specified (or newly built) image.

Take a look at the script to understand where each file is used.

# Running the Script

The script can take in a number of settings, but only really requires the cluster
name and zone. You can provide other settings for if we incorrectly deduce any
values. Usage:

```shell
# Must be in the script directory
cd docs/deploy/gke
./gke-self-managed.sh -n CLUSTER_NAME -z ZONE
```

For other options, see the `--help` output.

Can speed things up if you've already pushed an image (or want to use a specific
version) with `--image-url PATH` (eg,
`--image-url k8s.gcr.io/ingress-gce-glbc-amd64:v1.5.2`). Can also set the `REGISTRY`
env var to provide a custom place to push your image to if it's not the same project
as your cluster is in. The tags pushed to will be that of the
`git describe --tags --always --dirty` command. If the `VERSION` env var is set, that
will be used as the tag.

# Common Issues

One common issue is that the script outputs an error indicating that something
went wrong with permissions. The quick fix for this is to make sure that during
the execution of the script, the logged-in account you see in gcloud should be
the project owner. We say for the duration of the script because potentially
the role for the account could change mid-execution (ex. fat finger in GCP UI).
issue.

If you have issues with the controller after the script execution and you do not
know what it causing it, invoke the script in its cleanup mode. The is a quick
and simple way of going back to how everything was before.
