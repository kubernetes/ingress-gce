package instancegroups

import (
	"reflect"
	"testing"

	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/test"
	namerutil "k8s.io/ingress-gce/pkg/utils/namer"
)

var (
	clusterUID       = "cluster-1"
	fakeZone         = "zone-a"
	defaultMaxIGSize = 1000
)

func newMultiInstancesGroupController(fakeInstanceGroups *FakeInstanceGroups, maxIGSize int) *multiIGManager {
	fakeZL := &FakeZoneLister{Zones: []string{fakeZone}}
	namer := namerutil.NewNamer(clusterUID, "")

	multiIGManager := &multiIGManager{
		manager: &manager{
			cloud:              fakeInstanceGroups,
			namer:              namer,
			recorder:           (&test.FakeRecorderSource{}).Recorder(""),
			instanceLinkFormat: basePath + "zones/%s/instances/%s",
			ZoneLister:         fakeZL,
			maxIGSize:          maxIGSize,
		},
		maxNumberOfIGs: 15,
	}

	return multiIGManager
}

func TestCalculateDataPerZone(t *testing.T) {
	namer := namerutil.NewNamer(clusterUID, "")

	testIG0 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(0), Zone: fakeZone}
	testIG1 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(1), Zone: fakeZone}
	userCreatedIG := &compute.InstanceGroup{Name: "user-created-IG", Zone: fakeZone}

	instance1 := "instance-1"
	instance2 := "instance-2"
	instance3 := "instance-3"
	instance4 := "instance-4"
	instance5 := "instance-5"
	instance6 := "instance-6"

	testCases := []struct {
		name                   string
		zonalNodeNames         []string
		IGToInstances          IGsToInstances
		expectedNodesToIGSizes map[string]int
		expectedIGSizes        map[string]int
		expectedNodesToRemove  []string
		expectedNodesToAdd     []string
	}{
		{
			name:                   "0 instances",
			zonalNodeNames:         []string{},
			IGToInstances:          IGsToInstances{},
			expectedNodesToIGSizes: map[string]int{},
			expectedIGSizes:        map[string]int{},
			expectedNodesToRemove:  []string{},
			expectedNodesToAdd:     []string{},
		},
		{
			name: "1 existent instance in single IG",
			IGToInstances: IGsToInstances{
				testIG0: sets.NewString(instance1),
			},
			zonalNodeNames: []string{instance1},
			expectedNodesToIGSizes: map[string]int{
				instance1: 1,
			},
			expectedIGSizes: map[string]int{
				testIG0.Name: 1,
			},
			expectedNodesToRemove: []string{},
			expectedNodesToAdd:    []string{},
		},
		{
			name: "2 existent instances in same IG",
			IGToInstances: IGsToInstances{
				testIG0: sets.NewString(instance1, instance2),
			},
			zonalNodeNames: []string{instance1, instance2},
			expectedNodesToIGSizes: map[string]int{
				instance1: 1,
				instance2: 1,
			},
			expectedIGSizes: map[string]int{
				testIG0.Name: 2,
			},
			expectedNodesToRemove: []string{},
			expectedNodesToAdd:    []string{},
		},
		{
			name:                   "2 new instances",
			IGToInstances:          IGsToInstances{},
			zonalNodeNames:         []string{instance1, instance2},
			expectedNodesToIGSizes: map[string]int{},
			expectedIGSizes:        map[string]int{},
			expectedNodesToRemove:  []string{},
			expectedNodesToAdd:     []string{instance1, instance2},
		},
		{
			name: "2 old instances",
			IGToInstances: IGsToInstances{
				testIG0: sets.NewString(instance1, instance2),
			},
			zonalNodeNames: []string{},
			expectedNodesToIGSizes: map[string]int{
				instance1: 1,
				instance2: 1,
			},
			expectedIGSizes: map[string]int{
				testIG0.Name: 2,
			},
			expectedNodesToRemove: []string{instance1, instance2},
			expectedNodesToAdd:    []string{},
		},
		{
			name: "1 instance in non cluster IG",
			IGToInstances: IGsToInstances{
				userCreatedIG: sets.NewString(instance1),
			},
			zonalNodeNames:         []string{},
			expectedNodesToIGSizes: map[string]int{},
			expectedIGSizes:        map[string]int{},
			expectedNodesToRemove:  []string{},
			expectedNodesToAdd:     []string{},
		},
		{
			name: "2 existent instances in different IGs",
			IGToInstances: IGsToInstances{
				testIG0: sets.NewString(instance1),
				testIG1: sets.NewString(instance2),
			},
			zonalNodeNames: []string{instance1, instance2},
			expectedNodesToIGSizes: map[string]int{
				instance1: 1,
				instance2: 1,
			},
			expectedIGSizes: map[string]int{
				testIG0.Name: 1,
				testIG1.Name: 1,
			},
			expectedNodesToRemove: []string{},
			expectedNodesToAdd:    []string{},
		},
		{
			name: "Multiple IG, multiple instance, with one user-created IG",
			IGToInstances: IGsToInstances{
				testIG0:       sets.NewString(instance1, instance2, instance5),
				testIG1:       sets.NewString(instance3),
				userCreatedIG: sets.NewString(instance6),
			},
			zonalNodeNames: []string{instance1, instance3, instance4},
			expectedNodesToIGSizes: map[string]int{
				instance1: 1,
				instance2: 1,
				instance3: 1,
				instance5: 1,
			},
			expectedIGSizes: map[string]int{
				testIG0.Name: 3,
				testIG1.Name: 1,
			},
			expectedNodesToRemove: []string{instance2, instance5},
			expectedNodesToAdd:    []string{instance4},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {

			zonesToIGs := map[string]IGsToInstances{
				fakeZone: testCase.IGToInstances,
			}
			igc := newMultiInstancesGroupController(NewFakeInstanceGroups(zonesToIGs, defaultMaxIGSize), defaultMaxIGSize)
			err, nodesToIG, igSizes, nodesToRemove, nodesToAdd := igc.calculateDataPerZone(
				fakeZone, testCase.zonalNodeNames,
			)
			if err != nil {
				t.Fatalf("Error while calculating data per zone: %v", err)
			}
			nodesToIGSizes := map[string]int{}
			for node := range nodesToIG {
				nodesToIGSizes[node]++
			}
			if !reflect.DeepEqual(testCase.expectedNodesToIGSizes, nodesToIGSizes) {
				t.Errorf("expectedNodesToIGSizes: %v, got: %v", testCase.expectedNodesToIGSizes, nodesToIG)
			}
			if !reflect.DeepEqual(testCase.expectedIGSizes, igSizes) {
				t.Errorf("expectedIGSizes: %v, got: %v", testCase.expectedIGSizes, igSizes)
			}
			if !reflect.DeepEqual(testCase.expectedNodesToRemove, nodesToRemove) {
				t.Errorf("expectedNodesToRemove: %v, got: %v", testCase.expectedNodesToRemove, nodesToRemove)
			}
			if !reflect.DeepEqual(testCase.expectedNodesToAdd, nodesToAdd) {
				t.Errorf("expectedNodesToAdd: %v, got: %v", testCase.expectedNodesToAdd, nodesToAdd)
			}
		})
	}
}

func TestRemoveNodes(t *testing.T) {
	namer := namerutil.NewNamer(clusterUID, "")

	testIG0 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(0), Zone: fakeZone}
	testIG1 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(1), Zone: fakeZone}
	testIG2 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(2), Zone: fakeZone}

	instance1 := "instance-1"
	instance2 := "instance-2"
	instance3 := "instance-3"
	instance4 := "instance-4"
	instance5 := "instance-5"
	instance6 := "instance-6"

	testCases := []struct {
		name                          string
		zonesToIGsToInstances         map[string]IGsToInstances
		removeNodes                   []string
		nodesToIG                     map[string]*compute.InstanceGroup
		expectedZonesToIGsToInstances map[string]IGsToInstances
	}{
		{
			name: "Removing single instance",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1),
				},
			},
			removeNodes: []string{instance1},
			nodesToIG: map[string]*compute.InstanceGroup{
				instance1: testIG0,
			},
			expectedZonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(),
				},
			},
		},
		{
			name: "Removing single instance when ig has multiple instances",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1, instance2, instance3),
				},
			},
			removeNodes: []string{instance2},
			nodesToIG: map[string]*compute.InstanceGroup{
				instance1: testIG0,
				instance2: testIG0,
				instance3: testIG0,
			},
			expectedZonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1, instance3),
				},
			},
		},
		{
			name: "Removing duplicate instance from IGs with many instances",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1, instance2, instance3),
					testIG1: sets.NewString(instance4, instance5),
					testIG2: sets.NewString(instance6),
				},
			},
			removeNodes: []string{},
			nodesToIG: map[string]*compute.InstanceGroup{
				instance1: testIG0,
				instance2: testIG0,
				instance3: testIG0,
				instance4: testIG1,
				instance5: testIG1,
				instance6: testIG2,
			},
			expectedZonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1, instance2, instance3),
					testIG1: sets.NewString(instance4, instance5),
					testIG2: sets.NewString(instance6),
				},
			},
		},
		{
			name: "Removing instances from multiple igs",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1, instance2, instance3),
					testIG1: sets.NewString(instance4, instance5),
					testIG2: sets.NewString(instance6),
				},
			},
			removeNodes: []string{instance1, instance2, instance6},
			nodesToIG: map[string]*compute.InstanceGroup{
				instance1: testIG0,
				instance2: testIG0,
				instance3: testIG0,
				instance4: testIG1,
				instance5: testIG1,
				instance6: testIG2,
			},
			expectedZonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance3),
					testIG1: sets.NewString(instance4, instance5),
					testIG2: sets.NewString(),
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fakeInstanceGroups := NewFakeInstanceGroups(testCase.zonesToIGsToInstances, 1000)
			igc := newMultiInstancesGroupController(fakeInstanceGroups, defaultMaxIGSize)

			err := igc.removeNodes(testCase.removeNodes, fakeZone, testCase.nodesToIG)
			if err != nil {
				t.Fatalf("Error while removing nodes: %v", err)
			}

			if !reflect.DeepEqual(fakeInstanceGroups.zonesToIGsToInstances, testCase.expectedZonesToIGsToInstances) {
				t.Errorf("Expected zonesToIGsToInstances: %v, got: %v", testCase.expectedZonesToIGsToInstances, fakeInstanceGroups.zonesToIGsToInstances)
			}
		})
	}
}

func TestAddNodes(t *testing.T) {
	namer := namerutil.NewNamer(clusterUID, "")

	testIG0 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(0), Zone: fakeZone}
	testIG1 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(1), Zone: fakeZone}
	testIG2 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(2), Zone: fakeZone}

	instance1 := "instance-1"
	instance2 := "instance-2"
	instance3 := "instance-3"
	instance4 := "instance-4"
	instance5 := "instance-5"

	testCases := []struct {
		name                               string
		zonesToIGsToInstances              map[string]IGsToInstances
		addNodes                           []string
		igSizes                            map[string]int
		expectedZonesToIGsNamesToInstances map[string]map[string]sets.String
		maxIGSize                          int
	}{
		{
			name:                  "Add 1 instance when no IG exits",
			zonesToIGsToInstances: map[string]IGsToInstances{},
			addNodes:              []string{instance1},
			igSizes:               map[string]int{},
			expectedZonesToIGsNamesToInstances: map[string]map[string]sets.String{
				fakeZone: {
					testIG0.Name: sets.NewString(instance1),
				},
			},
			maxIGSize: 1,
		},
		{
			name: "Add 1 instance when full IG exists",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1, instance2),
				},
			},
			addNodes: []string{instance3},
			igSizes: map[string]int{
				testIG0.Name: 2,
			},
			expectedZonesToIGsNamesToInstances: map[string]map[string]sets.String{
				fakeZone: {
					testIG0.Name: sets.NewString(instance1, instance2),
					testIG1.Name: sets.NewString(instance3),
				},
			},
			maxIGSize: 2,
		},
		{
			name: "Add 2 instances when exists IG with 1 available space",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1),
				},
			},
			addNodes: []string{instance2, instance3},
			igSizes: map[string]int{
				testIG0.Name: 1,
			},
			expectedZonesToIGsNamesToInstances: map[string]map[string]sets.String{
				fakeZone: {
					testIG0.Name: sets.NewString(instance1, instance2),
					testIG1.Name: sets.NewString(instance3),
				},
			},
			maxIGSize: 2,
		},
		{
			name: "Add instance when exists 2 full IGs",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1),
					testIG1: sets.NewString(instance2),
				},
			},
			addNodes: []string{instance3},
			igSizes: map[string]int{
				testIG0.Name: 1,
				testIG1.Name: 1,
			},
			expectedZonesToIGsNamesToInstances: map[string]map[string]sets.String{
				fakeZone: {
					testIG0.Name: sets.NewString(instance1),
					testIG1.Name: sets.NewString(instance2),
					testIG2.Name: sets.NewString(instance3),
				},
			},
			maxIGSize: 1,
		},
		{
			name: "Add instances when exists multiple partly free IGs",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1),
					testIG1: sets.NewString(instance2),
				},
			},
			addNodes: []string{instance3, instance4, instance5},
			igSizes: map[string]int{
				testIG0.Name: 1,
				testIG1.Name: 1,
			},
			expectedZonesToIGsNamesToInstances: map[string]map[string]sets.String{
				fakeZone: {
					testIG0.Name: sets.NewString(instance1, instance3),
					testIG1.Name: sets.NewString(instance2, instance4),
					testIG2.Name: sets.NewString(instance5),
				},
			},
			maxIGSize: 2,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fakeInstanceGroups := NewFakeInstanceGroups(testCase.zonesToIGsToInstances, testCase.maxIGSize)
			igc := newMultiInstancesGroupController(fakeInstanceGroups, testCase.maxIGSize)

			err := igc.addNodes(testCase.addNodes, fakeZone, testCase.igSizes)
			if err != nil {
				t.Fatalf("Error while adding nodes: %v", err)
			}

			zonesToIGsNamesToInstances := fakeInstanceGroups.ToIGNames()
			if !reflect.DeepEqual(zonesToIGsNamesToInstances, testCase.expectedZonesToIGsNamesToInstances) {
				t.Errorf("Expected zonesToIGsToInstances: %v, got: %v", testCase.expectedZonesToIGsNamesToInstances, zonesToIGsNamesToInstances)
			}
		})
	}
}

func TestSyncZone(t *testing.T) {
	namer := namerutil.NewNamer(clusterUID, "")

	testIG0 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(0), Zone: fakeZone}
	testIG1 := &compute.InstanceGroup{Name: namer.InstanceGroupByIndex(1), Zone: fakeZone}

	instance1 := "instance-1"
	instance2 := "instance-2"
	instance3 := "instance-3"
	instance4 := "instance-4"
	instance5 := "instance-5"
	instance6 := "instance-5"

	testCases := []struct {
		name                  string
		zonesToIGsToInstances map[string]IGsToInstances
		zonalNodeNames        []string
		// expectedZonesToIGsNamesToInstances can be used only if zonesToIGsToInstances does not contain duplicate nodes,
		// otherwise we can not be sure in which IG duplicate node will stay, cause of non-deterministic golang map keys iteration order
		expectedZonesToIGsNamesToInstances map[string]map[string]sets.String
		maxIGSize                          int
	}{
		{
			name:                  "Add 1 instance to empty setup",
			zonesToIGsToInstances: map[string]IGsToInstances{},
			zonalNodeNames:        []string{instance1},
			expectedZonesToIGsNamesToInstances: map[string]map[string]sets.String{
				fakeZone: {
					testIG0.Name: sets.NewString(instance1),
				},
			},
			maxIGSize: 5,
		},
		{
			name: "Add 1 instance, remove 1 instance, leave 1 instance",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1, instance2),
				},
			},
			zonalNodeNames: []string{instance1, instance3},
			expectedZonesToIGsNamesToInstances: map[string]map[string]sets.String{
				fakeZone: {
					testIG0.Name: sets.NewString(instance1, instance3),
				},
			},
			maxIGSize: 5,
		},
		{
			name: "Multiple IGs",
			zonesToIGsToInstances: map[string]IGsToInstances{
				fakeZone: {
					testIG0: sets.NewString(instance1, instance2, instance3),
					testIG1: sets.NewString(instance4, instance5),
				},
			},
			zonalNodeNames: []string{instance1, instance3, instance5, instance6},
			maxIGSize:      3,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fakeInstanceGroups := NewFakeInstanceGroups(testCase.zonesToIGsToInstances, 1000)
			igc := newMultiInstancesGroupController(fakeInstanceGroups, testCase.maxIGSize)

			err := igc.syncZone(fakeZone, testCase.zonalNodeNames)
			if err != nil {
				t.Fatalf("Error while removing nodes: %v", err)
			}

			resultZonesToIGsNamesToInstances := fakeInstanceGroups.ToIGNames()
			if testCase.expectedZonesToIGsNamesToInstances != nil && !reflect.DeepEqual(resultZonesToIGsNamesToInstances, testCase.expectedZonesToIGsNamesToInstances) {
				t.Errorf("Expected zonesToIGsToInstances: %v, got: %v", testCase.expectedZonesToIGsNamesToInstances, resultZonesToIGsNamesToInstances)
			}

			existingInstances := sets.NewString()
			for _, igs := range resultZonesToIGsNamesToInstances {
				for ig, IGInstances := range igs {
					if len(IGInstances) > testCase.maxIGSize {
						t.Errorf("IG: %v has %v instances, more than maxIGSize: %v", ig, len(IGInstances), testCase.maxIGSize)
					}
					for instance := range IGInstances {
						if existingInstances.Has(instance) {
							t.Errorf("Instance %v exists in multiple IGs", instance)
						}
						existingInstances.Insert(instance)
					}
				}
			}
			setRequiredNodes := sets.NewString(testCase.zonalNodeNames...)
			if !reflect.DeepEqual(existingInstances, setRequiredNodes) {
				t.Errorf("Result IGs includes following nodes: %v, required: %v", existingInstances, setRequiredNodes)
			}
		})
	}
}
