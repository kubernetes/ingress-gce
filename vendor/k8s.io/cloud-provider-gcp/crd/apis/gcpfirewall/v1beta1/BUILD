load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "v1beta1",
    srcs = [
        "doc.go",
        "types.go",
        "zz_generated.deepcopy.go",
        "zz_generated.register.go",
    ],
    importpath = "k8s.io/cloud-provider-gcp/crd/apis/gcpfirewall/v1beta1",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/k8s.io/apimachinery/pkg/apis/meta/v1:meta",
        "//vendor/k8s.io/apimachinery/pkg/runtime",
        "//vendor/k8s.io/apimachinery/pkg/runtime/schema",
    ],
)
