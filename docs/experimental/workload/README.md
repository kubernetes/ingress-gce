# Workload User Guide

## Overview

This guide describe the process to add a GCE instance as an external workload
to a GKE cluster with the workload controller.
The steps below assume that you already have a project
in GCP setup and that you have working `gcloud` and `kubectl` binaries.

Note: In the product-ready version, we should have binaries and containers
uploaded. Therefore, building process is omitted here.

## Installation

### Step 0: prepare a cluster

The following command creates a Kubernetes cluster named `example-cluster` in zone `us-west2-a`.
```bash
gcloud container clusters create example-cluster \
    --zone us-west2-a \
    --release-channel rapid
```

To get credentials for the local machine to connect to the cluster:
```bash
gcloud container clusters get-credentials example-cluster \
    --zone us-west2-a
```

### Step 1: apply RBAC configuration

Use the following commands to create ServiceAccounts and ClusterRoles:
```bash
kubectl apply -f docs/experimental/workload/rbac.yaml
```
In the command above, two service accounts and ClusterRoles are created
- `workload-controller` will be used by the workload controller.
- `workload` can be used by VM instances.

### Step 2: deploy the workload controller

Use the following commands to deploy the workload controller:
```bash
kubectl apply -f docs/experimental/workload/controller-deploy.yaml
```

### Step 3: create VM instance template

There are two ways for the workload to be authenticated in a Kubernetes
cluster: using an IAM service account or a Kubernetes service account.

#### Step 3a: use IAM service account

First, use the following command to create an IAM service account
`workload-test` with `container.clusterViewer` role for the project
`project-name`:
```bash
gcloud iam service-accounts create workload-test \
    --description="Demo the workload controller" \
    --display-name="workload-test"
gcloud projects add-iam-policy-binding project-name \
    --member=workload-test@project-name.iam.gserviceaccount.com \
    --role=roles/container.clusterViewer
```

Memorize the unique ID of this service account:
```bash
gcloud iam service-accounts describe \
    workload-test@project-name.iam.gserviceaccount.com
```

In the Kubernetes cluster, bind this service account with `workload` ClusterRole, as follows:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: workload-binding # Replace this with a name for this binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: workload
subjects:
  - kind: User
    name: "000000000000000000000" # Replace this with the unique id of the service account
```

At last, create an instance template `workload-temp` as follows:
```bash
gcloud compute instance-templates create workload-temp \
  --machine-type=n1-standard-1 \
  --boot-disk-size=10GB \
  --image-family=debian-10 \
  --image-project=debian-cloud \
  --scopes=https://www.googleapis.com/auth/cloud-platform \
  --service-account=workload-test@project-name.iam.gserviceaccount.com \
  --metadata=k8s-cluster-name=example-cluster,\
k8s-cluster-zone=us-west2-a,\
k8s-label-workload=vm,\
startup-script="#! /bin/bash

# The following lines start a HTTP hostname service
sudo apt-get update -y
sudo apt-get install apache2 -y
sudo service apache2 restart
sudo mkdir -p /var/www/html/
sudo /bin/hostname | sudo tee /var/www/html/index.html
sleep 1

# The following lines download the daemon and start it as a service
sudo gsutil cp gs://xinyu-vm-test/workload-daemon /root/workload-daemon
sudo chmod a+x /root/workload-daemon

sudo echo \"[Unit]
Description=K8s-VM-daemon

[Service]
Type=simple
Restart=always
RestartSec=1
ExecStart=/root/workload-daemon start

[Install]
WantedBy=multi-user.target\" > /etc/systemd/system/vmdaemon.service
sudo systemctl start vmdaemon"
```

In the command above, the following custom metadata are used:

| Metadata Key           | Value                              |
|------------------------|------------------------------------|
| k8s-cluster-name       | The name of the Kubernetes cluster |
| k8s-cluster-zone       | The zone of the Kubernetes cluster |
| k8s-label-{label-name} | Label value                        |

Here, all metadata entries starting with "k8s-label-" will be used as labels of Workload resource.
In the resource, the prefix "k8s-label-" is removed.

#### Step 3b: use Kubernetes service account

Get the access token of Kubernetes service account `workload`:
```bash
kubectl describe secret workload-token- | grep token:
```

Then, create an instance template `workload-temp` as follows:
```bash
gcloud compute instance-templates create workload-temp \
  --machine-type=n1-standard-1 \
  --boot-disk-size=10GB \
  --image-family=debian-10 \
  --image-project=debian-cloud \
  --scopes=https://www.googleapis.com/auth/cloud-platform \
  --metadata=k8s-cluster-name=example-cluster,\
k8s-cluster-zone=us-west2-a,\
k8s-sa-name=workload,\
k8s-sa-token="{access token}",\
k8s-label-workload=vm,\
startup-script="#! /bin/bash

# The following lines start a HTTP hostname service
sudo apt-get update -y
sudo apt-get install apache2 -y
sudo service apache2 restart
sudo mkdir -p /var/www/html/
sudo /bin/hostname | sudo tee /var/www/html/index.html
sleep 1

# The following lines download the daemon and start it as a service
sudo gsutil cp gs://xinyu-vm-test/workload-daemon /root/workload-daemon
sudo chmod a+x /root/workload-daemon

sudo echo \"[Unit]
Description=K8s-VM-daemon

[Service]
Type=simple
Restart=always
RestartSec=1
ExecStart=/root/workload-daemon start

[Install]
WantedBy=multi-user.target\" > /etc/systemd/system/vmdaemon.service
sudo systemctl start vmdaemon"
```

The following custom metadata are used:

| Metadata Key           | Value                                              |
|------------------------|----------------------------------------------------|
| k8s-sa-name            | The name of Kubernetes Service Account workload    |
| k8s-sa-token           | The access token of the Kubernetes Service Account |
| k8s-cluster-name       | The name of the Kubernetes cluster                 |
| k8s-cluster-zone       | The zone of the Kubernetes cluster                 |
| k8s-label-{label-name} | Label value                                        |

### Step 4: create managed instance group

Use the following command to create a managed instance group:
```bash
gcloud compute instance-groups managed create workload-mig \
  --zone us-west2-a \
  --size=1 \
  --template=workload-temp
```

After the instance is up, we can observe that a Workload resource is created in
the cluster:
```bash
kubectl get workload
```

The output should be like follows:
```text
NAME                AGE
workload-mig-3p1x   35s
```

### Step 5: create service

Kubernetes services whose selectors match with this workload can have it as a
backend. For example, use the following yaml to create a service:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: workload-service
spec:
  selector:
    workload: vm
  type: ClusterIP
  ports:
  - port: 80
```

Then, we can validate that the corresponding endpointslice is created:
```bash
kubectl get endpointslice
```

The output should be like follows:
```text
NAME                                          ADDRESSTYPE   PORTS   ENDPOINTS       AGE
workload-service-workload-controller.k8s.io   IPv4          80      10.168.15.209   91s
```

# Troubleshooting

## Service Account Email

By default the VM does not have the `userinfo-email` scope, so the Kubernetes
cluster can only recognize its unique ID, but not the email.
See [Role-based access control](https://cloud.google.com/kubernetes-engine/docs/how-to/role-based-access-control#forbidden_error_for_service_accounts_on_vm_instances)
for details.

## Firewall Setting

<!-- Write something here? -->
