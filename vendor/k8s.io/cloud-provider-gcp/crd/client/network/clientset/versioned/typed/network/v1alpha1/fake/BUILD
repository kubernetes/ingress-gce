load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "fake",
    srcs = [
        "doc.go",
        "fake_gkenetworkparamset.go",
        "fake_gkenetworkparamsetlist.go",
        "fake_network.go",
        "fake_network_client.go",
        "fake_networkinterface.go",
        "fake_networkinterfacelist.go",
        "fake_networklist.go",
    ],
    importpath = "k8s.io/cloud-provider-gcp/crd/client/network/clientset/versioned/typed/network/v1alpha1/fake",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/k8s.io/apimachinery/pkg/apis/meta/v1:meta",
        "//vendor/k8s.io/apimachinery/pkg/labels",
        "//vendor/k8s.io/apimachinery/pkg/runtime/schema",
        "//vendor/k8s.io/apimachinery/pkg/types",
        "//vendor/k8s.io/apimachinery/pkg/watch",
        "//vendor/k8s.io/client-go/rest",
        "//vendor/k8s.io/client-go/testing",
        "//vendor/k8s.io/cloud-provider-gcp/crd/apis/network/v1alpha1",
        "//vendor/k8s.io/cloud-provider-gcp/crd/client/network/clientset/versioned/typed/network/v1alpha1",
    ],
)
