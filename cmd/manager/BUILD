load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//apiserver:go_default_library",
        "//cmd:go_default_library",
        "//cmd/version:go_default_library",
        "//model:go_default_library",
        "//platform/kube:go_default_library",
        "//proxy/envoy:go_default_library",
        "@com_github_davecgh_go_spew//spew:go_default_library",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_hashicorp_go_multierror//:go_default_library",
        "@com_github_spf13_cobra//:go_default_library",
        "@io_istio_api//:go_default_library",
    ],
)

go_binary(
    name = "manager",
    library = ":go_default_library",
    linkstamp = "istio.io/manager/cmd/version",
    visibility = ["//visibility:public"],
)
