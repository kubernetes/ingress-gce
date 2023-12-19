package utils

import (
	"k8s.io/klog/v2"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

func TestNEGName(t *testing.T) {
	shortNamespace := "namespace"
	shortName := "name"
	longNamespace := "namespacenamespacenamespacenamespacenamespace"
	longName := "namenamenamenamenamenamenamenamename"

	defaultNamer := namer.NewNamer("uid1", "", klog.TODO())
	l4Namer := namer.NewL4Namer("uid1", defaultNamer)

	testCases := []struct {
		desc        string
		svcPort     ServicePort
		wantNEGName string
	}{
		{
			desc: "short L4 ILB",
			svcPort: ServicePort{
				VMIPNEGEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: shortNamespace,
						Name:      shortName,
					},
				},
				BackendNamer: l4Namer,
			},
			wantNEGName: "k8s2-rrsi7zsy-namespace-name-v6mfehls",
		},
		{
			desc: "long L4 ILB",
			svcPort: ServicePort{
				VMIPNEGEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: longNamespace,
						Name:      longName,
					},
				},
				BackendNamer: l4Namer,
			},
			wantNEGName: "k8s2-rrsi7zsy-namespacenamespacename-namenamenamenamen-mxpkshn0",
		},
		{
			desc: "short L4 RBS",
			svcPort: ServicePort{
				L4RBSEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: shortNamespace,
						Name:      shortName,
					},
				},
				BackendNamer: l4Namer,
			},
			wantNEGName: "k8s2-rrsi7zsy-namespace-name-v6mfehls",
		},
		{
			desc: "long L4 RBS",
			svcPort: ServicePort{
				L4RBSEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: longNamespace,
						Name:      longName,
					},
				},
				BackendNamer: l4Namer,
			},
			wantNEGName: "k8s2-rrsi7zsy-namespacenamespacename-namenamenamenamen-mxpkshn0",
		},
		{
			desc: "short Ingress",
			svcPort: ServicePort{
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: shortNamespace,
						Name:      shortName,
					},
				},
				BackendNamer: defaultNamer,
				Port:         123,
			},
			wantNEGName: "k8s1-uid1-namespace-name-123-35a3c5b0",
		},
		{
			desc: "long Ingress",
			svcPort: ServicePort{
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: longNamespace,
						Name:      longName,
					},
				},
				BackendNamer: defaultNamer,
				Port:         123,
			},
			wantNEGName: "k8s1-uid1-namespacenamespacenam-namenamenamename-1-e3670135",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			gotName := tc.svcPort.NEGName()

			if gotName != tc.wantNEGName {
				t.Errorf("Wrong NEGName for svcPort %v. Got: %s, want: %s", tc.svcPort, gotName, tc.wantNEGName)
			}
		})
	}
}

func TestBackendName(t *testing.T) {
	shortNamespace := "namespace"
	shortName := "name"
	longNamespace := "namespacenamespacenamespacenamespacenamespace"
	longName := "namenamenamenamenamenamenamenamename"

	defaultNamer := namer.NewNamer("uid1", "", klog.TODO())
	l4Namer := namer.NewL4Namer("uid1", defaultNamer)

	testCases := []struct {
		desc            string
		svcPort         ServicePort
		wantBackendName string
	}{
		{
			desc: "short L4 ILB",
			svcPort: ServicePort{
				VMIPNEGEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: shortNamespace,
						Name:      shortName,
					},
				},
				BackendNamer: l4Namer,
			},
			wantBackendName: "k8s2-rrsi7zsy-namespace-name-v6mfehls",
		},
		{
			desc: "long L4 ILB",
			svcPort: ServicePort{
				VMIPNEGEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: longNamespace,
						Name:      longName,
					},
				},
				BackendNamer: l4Namer,
			},
			wantBackendName: "k8s2-rrsi7zsy-namespacenamespacename-namenamenamenamen-mxpkshn0",
		},
		{
			desc: "short L4 RBS with NEG",
			svcPort: ServicePort{
				L4RBSEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: shortNamespace,
						Name:      shortName,
					},
				},
				BackendNamer: l4Namer,
			},
			wantBackendName: "k8s2-rrsi7zsy-namespace-name-v6mfehls",
		},
		{
			desc: "long L4 RBS with NEG",
			svcPort: ServicePort{
				L4RBSEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: longNamespace,
						Name:      longName,
					},
				},
				BackendNamer: l4Namer,
			},
			wantBackendName: "k8s2-rrsi7zsy-namespacenamespacename-namenamenamenamen-mxpkshn0",
		},
		{
			desc: "short RXLB Ingress",
			svcPort: ServicePort{
				L7XLBRegionalEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: shortNamespace,
						Name:      shortName,
					},
				},
				BackendNamer: defaultNamer,
				Port:         123,
			},
			wantBackendName: "k8s1-uid1-e-namespace-name-123-35a3c5b0",
		},
		{
			desc: "long RXLB Ingress",
			svcPort: ServicePort{
				L7XLBRegionalEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: longNamespace,
						Name:      longName,
					},
				},
				BackendNamer: defaultNamer,
				Port:         123,
			},
			wantBackendName: "k8s1-uid1-e-namespacenamespacena-namenamenamenam-1-e3670135",
		},
		{
			desc: "short Ingress",
			svcPort: ServicePort{
				NEGEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: shortNamespace,
						Name:      shortName,
					},
				},
				BackendNamer: defaultNamer,
				Port:         123,
			},
			wantBackendName: "k8s1-uid1-namespace-name-123-35a3c5b0",
		},
		{
			desc: "long Ingress",
			svcPort: ServicePort{
				NEGEnabled: true,
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: longNamespace,
						Name:      longName,
					},
				},
				BackendNamer: defaultNamer,
				Port:         123,
			},
			wantBackendName: "k8s1-uid1-namespacenamespacenam-namenamenamename-1-e3670135",
		},
		{
			desc: "short Instance Group",
			svcPort: ServicePort{
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: shortNamespace,
						Name:      shortName,
					},
				},
				BackendNamer: defaultNamer,
				Port:         123,
			},
			wantBackendName: "k8s-be-0--uid1",
		},
		{
			desc: "long Instance Group",
			svcPort: ServicePort{
				ID: ServicePortID{
					Service: types.NamespacedName{
						Namespace: longNamespace,
						Name:      longName,
					},
				},
				BackendNamer: defaultNamer,
				Port:         123,
			},
			wantBackendName: "k8s-be-0--uid1",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			gotName := tc.svcPort.BackendName()

			if gotName != tc.wantBackendName {
				t.Errorf("Wrong BackendName for svcPort %v. Got: %s, want: %s", tc.svcPort, gotName, tc.wantBackendName)
			}
		})
	}
}
