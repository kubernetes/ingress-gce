# Overview

Welcome, you are reading this because you are interested in running a version of the
GCP Ingress Controller (GLBC) before it is officially released on GKE! The purpose of this is to
allow users to find bugs and report them, while also getting early access to improvements and
new features. You will notice that the following things are sitting in this directory:

1. script.sh
2. gce.conf
3. yaml/

We will explain what each of these things mean in a bit. However, you will only be interacting
with one file (script.sh).

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
is needed? Well, the reason is that the script invokes a kubectl command which
creates a new k8s RBAC role (see below for explanation why). In order to do this, the
current user must be the project owner. The "Owner" role also gives the account
permission to do basically anything so all commands the script runs should
theoretically work. If not, the script will do its best to fail gracefully
and let you know what might have went wrong.

The second step is to make sure you populate the gce.conf file. The instructions
for populating the file are in the file itself. You just have to fill it in.

# Important Details

Most likely, you want to know what this script is doing to your cluster in order
to run the new controller and why it is doing it. If you do not, care then you
can go ahead and skip this section.

Here is a brief summary of each major thing we do and why:

1. Turn off GLBC and turn on new GLBC in the cluster
    * To be brief, the maintenance cost of running a new controller on the master
      is actually pretty high. This is why we chose to move the controller
      to the cluster.
1. Create a new k8s RBAC role
    * On the master, the GLBC has unauthenticated access to the k8s API server.
      Once we move the GLBC to the cluster, that path is gone. Therefore, we need to
      configure a new RBAC role that allows GLBC the same access.
3. Create new GCP service account + key
    * On the master, the GLBC is authorized to use the GCP Compute API through a
      token pulled from a private GKE endpoint. Moving to the cluster will result in
      us not being able to utilize this. Therefore, we need to create a new GCP
      service account and a corresponding key which will grant access.
4. Start new GLBC (and default backend) in the cluster
    * As stated before, we need to run the GLBC in the cluster. We also need to
      startup a new default backend because the mechanism we use to turn off the
      master GLBC removes both the GLBC and the default backend.
    * Because we have to recreate the default backend, there will be a small
      segment of time when requests to the default backend will time out.

The script is commented heavily, so it should be pretty easy to follow along
with what we described above.

## Dependencies

As promised, here is an explanation of each script dependency.

1. gce.conf
    * This file normally sits on the GKE master and provides important config for
      the GCP Compute API client within the GLBC. The GLBC is configured to know
      where to look for this file. In this case, we simply mount the file as a
      volume and tell GLBC to look for it there.
2. yaml/default-http-backend.yaml
    * This file contains the specifications for both the default-http-backend
      deployment and service. This is no different than what you are used to
      seeing in your cluster. In this case, we need to recreate the default
      backend since turning off the GLBC on the master removes it.
3. yaml/rbac.yaml
    * This file contains specification for an RBAC role which gives the GLBC
      access to the resources it needs from the k8s API server.
4. yaml/glbc.yaml
    * This file contains the specification for the GLBC deployment. Notice that in
      this case, we need a deployment because we want to preserve the controller
      in case of node restarts.

Take a look at the script to understand where each file is used.

# Running the Script

Run the command below to see the usage:

`./script.sh --help`

After that, it should be self-explanatory!

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
