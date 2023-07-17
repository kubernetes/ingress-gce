# Change Log

## [v1.24.0](https://github.com/kubernetes/ingress-gce/tree/v1.24.0) (2023-07-06)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.23.2...v1.24.0)

**Merged pull requests:**

-  Add instructions for check-gke-ingress and updated makefile [\#2184](https://github.com/kubernetes/ingress-gce/pull/2184) ([songrx1997](https://github.com/songrx1997))
- Update healthcheck Description on BackendConfig removal [\#2181](https://github.com/kubernetes/ingress-gce/pull/2181) ([DamianSawicki](https://github.com/DamianSawicki))

## [v1.23.2](https://github.com/kubernetes/ingress-gce/tree/v1.23.2) (2023-07-03)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.22.4...v1.23.2)

## [v1.22.4](https://github.com/kubernetes/ingress-gce/tree/v1.22.4) (2023-07-03)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.21.2...v1.22.4)

## [v1.21.2](https://github.com/kubernetes/ingress-gce/tree/v1.21.2) (2023-07-03)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.20.4...v1.21.2)

## [v1.20.4](https://github.com/kubernetes/ingress-gce/tree/v1.20.4) (2023-07-03)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.23.1...v1.20.4)

**Implemented enhancements:**

- BackendConfig.cdn.compressionMode not supported [\#1908](https://github.com/kubernetes/ingress-gce/issues/1908)
- Enabling global access for internal HTTP\(S\) load balancer [\#1883](https://github.com/kubernetes/ingress-gce/issues/1883)

**Fixed bugs:**

- L4 controller references cache objects [\#2135](https://github.com/kubernetes/ingress-gce/issues/2135)
- Ingress changes resulting in 502s [\#1756](https://github.com/kubernetes/ingress-gce/issues/1756)
- ingress-gce-404-server-with-metrics causes OOM [\#1426](https://github.com/kubernetes/ingress-gce/issues/1426)

**Closed issues:**

- What is the controller spec for an IngressClass to work with ingress-gce? [\#1891](https://github.com/kubernetes/ingress-gce/issues/1891)
- Custom Error Pages [\#1757](https://github.com/kubernetes/ingress-gce/issues/1757)

**Merged pull requests:**

- Include proxy-only subnet with purpose=REGIONAL\_MANAGED\_PROXY when generating firewall rules for Ingress [\#2186](https://github.com/kubernetes/ingress-gce/pull/2186) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add /96 suffix to static addresses for IPv6 external forwarding rules [\#2183](https://github.com/kubernetes/ingress-gce/pull/2183) ([panslava](https://github.com/panslava))
- Use field selector to get ASM configmap [\#2182](https://github.com/kubernetes/ingress-gce/pull/2182) ([linxiulei](https://github.com/linxiulei))
- Add support to enable single ingress check [\#2179](https://github.com/kubernetes/ingress-gce/pull/2179) ([songrx1997](https://github.com/songrx1997))
- Move common setup in l4 dualstack tests to separate function [\#2178](https://github.com/kubernetes/ingress-gce/pull/2178) ([panslava](https://github.com/panslava))
- Neg count metrics [\#2177](https://github.com/kubernetes/ingress-gce/pull/2177) ([swetharepakula](https://github.com/swetharepakula))
- Take only strategy keys in cloudprovideradapter for dynamic throttling [\#2176](https://github.com/kubernetes/ingress-gce/pull/2176) ([alexkats](https://github.com/alexkats))
- Add support for Custom Subnet for IPv6 NetLB [\#2174](https://github.com/kubernetes/ingress-gce/pull/2174) ([panslava](https://github.com/panslava))
- update k8s/gce server request metrics to count server error [\#2173](https://github.com/kubernetes/ingress-gce/pull/2173) ([sawsa307](https://github.com/sawsa307))
- Update loggings in syncers. [\#2172](https://github.com/kubernetes/ingress-gce/pull/2172) ([sawsa307](https://github.com/sawsa307))
- Multi networking support for NetLB [\#2171](https://github.com/kubernetes/ingress-gce/pull/2171) ([mmamczur](https://github.com/mmamczur))
- Add instruction doc for check-gke-ingress [\#2170](https://github.com/kubernetes/ingress-gce/pull/2170) ([songrx1997](https://github.com/songrx1997))
- chore: remove refs to deprecated io/ioutil [\#2169](https://github.com/kubernetes/ingress-gce/pull/2169) ([testwill](https://github.com/testwill))
- chore: unnecessary use of fmt.Sprintf \(S1039\) [\#2168](https://github.com/kubernetes/ingress-gce/pull/2168) ([testwill](https://github.com/testwill))
- fix: CVE-2022-41723 CVE-2022-41717 [\#2167](https://github.com/kubernetes/ingress-gce/pull/2167) ([testwill](https://github.com/testwill))
- Reserve Static IPv6 address before syncing L4 NetLB [\#2165](https://github.com/kubernetes/ingress-gce/pull/2165) ([panslava](https://github.com/panslava))
- Fix firewalls for L4 ILBs. [\#2163](https://github.com/kubernetes/ingress-gce/pull/2163) ([mmamczur](https://github.com/mmamczur))
- Add K8s Request Count Metric [\#2162](https://github.com/kubernetes/ingress-gce/pull/2162) ([sawsa307](https://github.com/sawsa307))
- Update logging in endpoint calculation. [\#2161](https://github.com/kubernetes/ingress-gce/pull/2161) ([sawsa307](https://github.com/sawsa307))
- Update syncer count metrics emit [\#2160](https://github.com/kubernetes/ingress-gce/pull/2160) ([sawsa307](https://github.com/sawsa307))
- Add frontendConfig annotation check before frontendConfig checks and update tests [\#2159](https://github.com/kubernetes/ingress-gce/pull/2159) ([songrx1997](https://github.com/songrx1997))
- Add Dockerfile for check-gke-ingress [\#2158](https://github.com/kubernetes/ingress-gce/pull/2158) ([songrx1997](https://github.com/songrx1997))
- Reserve Static IPv6 address before syncing L4 ILB [\#2157](https://github.com/kubernetes/ingress-gce/pull/2157) ([panslava](https://github.com/panslava))
- Add syncer in error state metrics [\#2156](https://github.com/kubernetes/ingress-gce/pull/2156) ([sawsa307](https://github.com/sawsa307))
- Modify the NEG controller to also create NEGs for L4 External Load Ba… [\#2155](https://github.com/kubernetes/ingress-gce/pull/2155) ([mmamczur](https://github.com/mmamczur))
- Cleanup NEG Metrics [\#2153](https://github.com/kubernetes/ingress-gce/pull/2153) ([swetharepakula](https://github.com/swetharepakula))
- Deep copy service after getting from informer [\#2152](https://github.com/kubernetes/ingress-gce/pull/2152) ([panslava](https://github.com/panslava))
- Fix vendor to match current dependencies [\#2151](https://github.com/kubernetes/ingress-gce/pull/2151) ([swetharepakula](https://github.com/swetharepakula))
- Add GCE Request Count Metric [\#2150](https://github.com/kubernetes/ingress-gce/pull/2150) ([swetharepakula](https://github.com/swetharepakula))
- Fix potential nil pointer panic in check-gke-ingress [\#2149](https://github.com/kubernetes/ingress-gce/pull/2149) ([songrx1997](https://github.com/songrx1997))
- set arm64 tolerations in ingress e2e tests [\#2148](https://github.com/kubernetes/ingress-gce/pull/2148) ([cezarygerard](https://github.com/cezarygerard))
- Add gauravkghildiyal to reviewers [\#2146](https://github.com/kubernetes/ingress-gce/pull/2146) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Delete RBS annotation explicitly in preventTargetPoolRaceWithRBSOnCreation [\#2145](https://github.com/kubernetes/ingress-gce/pull/2145) ([panslava](https://github.com/panslava))
- Push arch specific images [\#2144](https://github.com/kubernetes/ingress-gce/pull/2144) ([code-elinka](https://github.com/code-elinka))
- Integrate checks for check-gke-ingress [\#2142](https://github.com/kubernetes/ingress-gce/pull/2142) ([songrx1997](https://github.com/songrx1997))
- Pin golang patch version in Makefile [\#2140](https://github.com/kubernetes/ingress-gce/pull/2140) ([code-elinka](https://github.com/code-elinka))
- Use multiarch echo image for e2e tests [\#2139](https://github.com/kubernetes/ingress-gce/pull/2139) ([code-elinka](https://github.com/code-elinka))
- Endpoint State metrics Cleanup [\#2138](https://github.com/kubernetes/ingress-gce/pull/2138) ([sawsa307](https://github.com/sawsa307))
- Reset gauges when collecting service metrics [\#2137](https://github.com/kubernetes/ingress-gce/pull/2137) ([cezarygerard](https://github.com/cezarygerard))
- Reset gauges when collecting service metrics [\#2136](https://github.com/kubernetes/ingress-gce/pull/2136) ([cezarygerard](https://github.com/cezarygerard))
- Delete finalizer last in L4 RBS controller [\#2134](https://github.com/kubernetes/ingress-gce/pull/2134) ([panslava](https://github.com/panslava))
- Add CheckL7ILBNegAnnotation to check-gke-ingress [\#2133](https://github.com/kubernetes/ingress-gce/pull/2133) ([songrx1997](https://github.com/songrx1997))
- Remove running tests with verbose mode [\#2132](https://github.com/kubernetes/ingress-gce/pull/2132) ([panslava](https://github.com/panslava))
- Reset L4 DualStack Gauge metrics before exporting [\#2131](https://github.com/kubernetes/ingress-gce/pull/2131) ([panslava](https://github.com/panslava))
- Add CheckRuleHostOverwrite to check-gke-ingress [\#2130](https://github.com/kubernetes/ingress-gce/pull/2130) ([songrx1997](https://github.com/songrx1997))
- Add CheckAppProtocolAnnotation to check-gke-ingress [\#2129](https://github.com/kubernetes/ingress-gce/pull/2129) ([songrx1997](https://github.com/songrx1997))
- Allow IAM policy changes to not require user interactive prompt [\#2128](https://github.com/kubernetes/ingress-gce/pull/2128) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add CheckL7ILBFrontendConfig to check-gke-ingress [\#2127](https://github.com/kubernetes/ingress-gce/pull/2127) ([songrx1997](https://github.com/songrx1997))
- Change logic of deleting RBS service [\#2126](https://github.com/kubernetes/ingress-gce/pull/2126) ([panslava](https://github.com/panslava))
- Make minimal modifications to make dual-stack-negs work with self-managed controller [\#2125](https://github.com/kubernetes/ingress-gce/pull/2125) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Introduce dynamic throttling for NEG requests [\#2124](https://github.com/kubernetes/ingress-gce/pull/2124) ([alexkats](https://github.com/alexkats))
- Use only one buildx command for e2e-test [\#2123](https://github.com/kubernetes/ingress-gce/pull/2123) ([code-elinka](https://github.com/code-elinka))
- Add buckets for syncer staleness metrics [\#2122](https://github.com/kubernetes/ingress-gce/pull/2122) ([sawsa307](https://github.com/sawsa307))
- Use hack script for e2e test image building [\#2121](https://github.com/kubernetes/ingress-gce/pull/2121) ([code-elinka](https://github.com/code-elinka))
- Add CheckFrontendConfigExistence to check-gke-ingress [\#2120](https://github.com/kubernetes/ingress-gce/pull/2120) ([songrx1997](https://github.com/songrx1997))
- Add CheckIngressRule to check-gke-ingress [\#2119](https://github.com/kubernetes/ingress-gce/pull/2119) ([songrx1997](https://github.com/songrx1997))
- Update google.golang.org/api to v0.122.0 [\#2117](https://github.com/kubernetes/ingress-gce/pull/2117) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Integration tests for NEG DualStack Migrator [\#2116](https://github.com/kubernetes/ingress-gce/pull/2116) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Update go version to 1.20 to match the image build version [\#2115](https://github.com/kubernetes/ingress-gce/pull/2115) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
-  Use gcloud base image for e2e-test container [\#2114](https://github.com/kubernetes/ingress-gce/pull/2114) ([code-elinka](https://github.com/code-elinka))
- Change e2e-test dockerfile run cmds [\#2113](https://github.com/kubernetes/ingress-gce/pull/2113) ([code-elinka](https://github.com/code-elinka))
- Add CheckHealthCheckConfig rule to check-gke-ingress [\#2112](https://github.com/kubernetes/ingress-gce/pull/2112) ([songrx1997](https://github.com/songrx1997))
- Emit Events on THC \(mis\)configuration [\#2111](https://github.com/kubernetes/ingress-gce/pull/2111) ([DamianSawicki](https://github.com/DamianSawicki))
- Remove verbose arg from cloudbuild.yaml [\#2110](https://github.com/kubernetes/ingress-gce/pull/2110) ([code-elinka](https://github.com/code-elinka))
- Build binaries for docker images in cloudbuild.yaml [\#2108](https://github.com/kubernetes/ingress-gce/pull/2108) ([code-elinka](https://github.com/code-elinka))
- Use degraded mode results in NEG DualStack migration heuristics [\#2107](https://github.com/kubernetes/ingress-gce/pull/2107) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add CheckBackendConfigExistence to check-gke-ingress [\#2106](https://github.com/kubernetes/ingress-gce/pull/2106) ([songrx1997](https://github.com/songrx1997))
- Add CheckBackendConfigAnnotation rule to check-gke-ingress [\#2105](https://github.com/kubernetes/ingress-gce/pull/2105) ([songrx1997](https://github.com/songrx1997))
- Switch Compute API to beta for NEG IPv6 fields when DualStackNEG feature flag is enabled [\#2104](https://github.com/kubernetes/ingress-gce/pull/2104) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Rename syncer and endpoint metrics to match Monarch [\#2103](https://github.com/kubernetes/ingress-gce/pull/2103) ([sawsa307](https://github.com/sawsa307))
- bump timeout for building images [\#2101](https://github.com/kubernetes/ingress-gce/pull/2101) ([aojea](https://github.com/aojea))
- Rename gke-diagnoser to check-gke-ingress [\#2100](https://github.com/kubernetes/ingress-gce/pull/2100) ([songrx1997](https://github.com/songrx1997))
- Remove the flag EnableBackendConfigHealthCheck [\#2099](https://github.com/kubernetes/ingress-gce/pull/2099) ([DamianSawicki](https://github.com/DamianSawicki))
- Add flags for gke-ingress-checker [\#2098](https://github.com/kubernetes/ingress-gce/pull/2098) ([songrx1997](https://github.com/songrx1997))
- Track GCE/K8s server error [\#2097](https://github.com/kubernetes/ingress-gce/pull/2097) ([sawsa307](https://github.com/sawsa307))
- Jsonify description of healthchecks configured by Transparent Health Checks [\#2096](https://github.com/kubernetes/ingress-gce/pull/2096) ([DamianSawicki](https://github.com/DamianSawicki))
- Add new metrics for NEG DualStack Migrator [\#2095](https://github.com/kubernetes/ingress-gce/pull/2095) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Delete syncers from metrics collector during GC [\#2094](https://github.com/kubernetes/ingress-gce/pull/2094) ([sawsa307](https://github.com/sawsa307))
- Consume IPv6 Health status for NEG pod readiness [\#2093](https://github.com/kubernetes/ingress-gce/pull/2093) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Use Alpha NEG API when DualStackNEG feature is enabled [\#2092](https://github.com/kubernetes/ingress-gce/pull/2092) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Delete generated test binary [\#2091](https://github.com/kubernetes/ingress-gce/pull/2091) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Implement heuristics for detaching migration endpoints [\#2090](https://github.com/kubernetes/ingress-gce/pull/2090) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add validation for ipv6 only service [\#2089](https://github.com/kubernetes/ingress-gce/pull/2089) ([sawsa307](https://github.com/sawsa307))
- Add firewall rule for Transparent Health Checks [\#2088](https://github.com/kubernetes/ingress-gce/pull/2088) ([DamianSawicki](https://github.com/DamianSawicki))
- Change Dockerfile.echo base image [\#2087](https://github.com/kubernetes/ingress-gce/pull/2087) ([code-elinka](https://github.com/code-elinka))
- use cloudbuild to build container images [\#2086](https://github.com/kubernetes/ingress-gce/pull/2086) ([aojea](https://github.com/aojea))
- Change label propagation e2e test to align with label propagation config [\#2085](https://github.com/kubernetes/ingress-gce/pull/2085) ([songrx1997](https://github.com/songrx1997))
- Implement Pause and Continue for NEG DualStackMigrator [\#2084](https://github.com/kubernetes/ingress-gce/pull/2084) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Label propagation e2e test [\#2083](https://github.com/kubernetes/ingress-gce/pull/2083) ([songrx1997](https://github.com/songrx1997))
- Fix race condition in syncer test [\#2082](https://github.com/kubernetes/ingress-gce/pull/2082) ([sawsa307](https://github.com/sawsa307))
- Read THC annotation and configure UHC accordingly [\#2081](https://github.com/kubernetes/ingress-gce/pull/2081) ([DamianSawicki](https://github.com/DamianSawicki))
- Use time.Duration.String func to print duration [\#2080](https://github.com/kubernetes/ingress-gce/pull/2080) ([cezarygerard](https://github.com/cezarygerard))
- Fix wait.PollImmediate in TestEnableDegradedMode [\#2079](https://github.com/kubernetes/ingress-gce/pull/2079) ([sawsa307](https://github.com/sawsa307))
- Add network package containing the NetworkInfo struct [\#2078](https://github.com/kubernetes/ingress-gce/pull/2078) ([mmamczur](https://github.com/mmamczur))
- Update endpoint checking return [\#2075](https://github.com/kubernetes/ingress-gce/pull/2075) ([sawsa307](https://github.com/sawsa307))
- Define the interface for NEG dual stack migration handler [\#2073](https://github.com/kubernetes/ingress-gce/pull/2073) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Emit event on Description-only healthcheck update [\#2072](https://github.com/kubernetes/ingress-gce/pull/2072) ([DamianSawicki](https://github.com/DamianSawicki))
- Multi networking informers [\#2071](https://github.com/kubernetes/ingress-gce/pull/2071) ([mmamczur](https://github.com/mmamczur))
- JSONify healthcheck Description for BackendConfig [\#2068](https://github.com/kubernetes/ingress-gce/pull/2068) ([DamianSawicki](https://github.com/DamianSawicki))
- Degraded mode e2e [\#2066](https://github.com/kubernetes/ingress-gce/pull/2066) ([sawsa307](https://github.com/sawsa307))
- Degraded mode correctness metrics [\#2049](https://github.com/kubernetes/ingress-gce/pull/2049) ([sawsa307](https://github.com/sawsa307))
- Update error state set and reset [\#2039](https://github.com/kubernetes/ingress-gce/pull/2039) ([sawsa307](https://github.com/sawsa307))
- Neg sync metric [\#2034](https://github.com/kubernetes/ingress-gce/pull/2034) ([sawsa307](https://github.com/sawsa307))
- Add unit tests to validate error states are detected [\#2028](https://github.com/kubernetes/ingress-gce/pull/2028) ([sawsa307](https://github.com/sawsa307))
- Add CheckServiceExistence Rule for gke-ingress-checker [\#2025](https://github.com/kubernetes/ingress-gce/pull/2025) ([songrx1997](https://github.com/songrx1997))
- Multi networking neg controller [\#2023](https://github.com/kubernetes/ingress-gce/pull/2023) ([mmamczur](https://github.com/mmamczur))
- Add clientsets to gke-diagnoser tool [\#2022](https://github.com/kubernetes/ingress-gce/pull/2022) ([songrx1997](https://github.com/songrx1997))
- Add report utils for gke-diagnoser to support json output [\#2020](https://github.com/kubernetes/ingress-gce/pull/2020) ([songrx1997](https://github.com/songrx1997))
- Multi networking support in the L4 ILB controller [\#2013](https://github.com/kubernetes/ingress-gce/pull/2013) ([mmamczur](https://github.com/mmamczur))
- Filter pods that don't belong to the service in question [\#1966](https://github.com/kubernetes/ingress-gce/pull/1966) ([sawsa307](https://github.com/sawsa307))
- Filter pods that have out of range IP [\#1963](https://github.com/kubernetes/ingress-gce/pull/1963) ([sawsa307](https://github.com/sawsa307))
- Add metrics for endpoint and endpoint slice state [\#1919](https://github.com/kubernetes/ingress-gce/pull/1919) ([sawsa307](https://github.com/sawsa307))

## [v1.23.1](https://github.com/kubernetes/ingress-gce/tree/v1.23.1) (2023-04-17)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.23.0...v1.23.1)

## [v1.23.0](https://github.com/kubernetes/ingress-gce/tree/v1.23.0) (2023-04-17)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.22.3...v1.23.0)

**Merged pull requests:**

- Add label propagation metrics calculation logic [\#2077](https://github.com/kubernetes/ingress-gce/pull/2077) ([songrx1997](https://github.com/songrx1997))
- Add label propagation metrics [\#2076](https://github.com/kubernetes/ingress-gce/pull/2076) ([songrx1997](https://github.com/songrx1997))
- Add a script to push multiarch images [\#2074](https://github.com/kubernetes/ingress-gce/pull/2074) ([code-elinka](https://github.com/code-elinka))
- Filter out migrating endpoints in NEG controller [\#2070](https://github.com/kubernetes/ingress-gce/pull/2070) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add label propagation logic and update unit tests [\#2069](https://github.com/kubernetes/ingress-gce/pull/2069) ([songrx1997](https://github.com/songrx1997))
- Support IG backends removal in regional\_ig\_linker.go. [\#2067](https://github.com/kubernetes/ingress-gce/pull/2067) ([mmamczur](https://github.com/mmamczur))
- Fix a case when regional instance group linker has to add a backend t… [\#2063](https://github.com/kubernetes/ingress-gce/pull/2063) ([mmamczur](https://github.com/mmamczur))
- Set SampleRate to 0 when LogConfig is disabled for BackendService [\#2062](https://github.com/kubernetes/ingress-gce/pull/2062) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add IPv6 address when calculating target endpoints within L7EndpointsCalculator [\#2061](https://github.com/kubernetes/ingress-gce/pull/2061) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Revert "Set the k8s-cloud-provider API domain" [\#2060](https://github.com/kubernetes/ingress-gce/pull/2060) ([spencerhance](https://github.com/spencerhance))
- Fix small logs typo [\#2059](https://github.com/kubernetes/ingress-gce/pull/2059) ([panslava](https://github.com/panslava))
- Revert "Use Patch when syncing GCP BackendService. Linking backend groups still uses Update" [\#2057](https://github.com/kubernetes/ingress-gce/pull/2057) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Change diffBackends for NEG to not compare the API versions. [\#2054](https://github.com/kubernetes/ingress-gce/pull/2054) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Update set error state for CalculateEndpoints\(\) [\#2052](https://github.com/kubernetes/ingress-gce/pull/2052) ([sawsa307](https://github.com/sawsa307))
- Create run-make-cmd.sh [\#2046](https://github.com/kubernetes/ingress-gce/pull/2046) ([code-elinka](https://github.com/code-elinka))
- Refactor syncInternalImpl and toZoneNetworkEndpointMap [\#2044](https://github.com/kubernetes/ingress-gce/pull/2044) ([sawsa307](https://github.com/sawsa307))
- Use Patch when syncing GCP BackendService. Linking backend groups still uses Update [\#2043](https://github.com/kubernetes/ingress-gce/pull/2043) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add feature flag for IPv6 NEGs support [\#2021](https://github.com/kubernetes/ingress-gce/pull/2021) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add getPodLabelMap function for NegLabelPropagation and add unit test for it [\#1990](https://github.com/kubernetes/ingress-gce/pull/1990) ([songrx1997](https://github.com/songrx1997))
- Firewall CR migration [\#1626](https://github.com/kubernetes/ingress-gce/pull/1626) ([sugangli](https://github.com/sugangli))

## [v1.22.3](https://github.com/kubernetes/ingress-gce/tree/v1.22.3) (2023-03-31)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.22.2...v1.22.3)

**Merged pull requests:**

- Fix syncLock [\#2051](https://github.com/kubernetes/ingress-gce/pull/2051) ([sawsa307](https://github.com/sawsa307))
- Update k8s-cloud-provider to v1.23.0 for new IPv6 field in NEG API [\#2047](https://github.com/kubernetes/ingress-gce/pull/2047) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Refactor code related to PodLabelPropagationConfig [\#2045](https://github.com/kubernetes/ingress-gce/pull/2045) ([songrx1997](https://github.com/songrx1997))
- Add comparing PortRange in forwarding rules Equal function [\#2042](https://github.com/kubernetes/ingress-gce/pull/2042) ([panslava](https://github.com/panslava))
- don't log all nodenames [\#2041](https://github.com/kubernetes/ingress-gce/pull/2041) ([aojea](https://github.com/aojea))
- Set the k8s-cloud-provider API domain [\#2038](https://github.com/kubernetes/ingress-gce/pull/2038) ([spencerhance](https://github.com/spencerhance))
- Change e2e-test.yaml to support ARM [\#2037](https://github.com/kubernetes/ingress-gce/pull/2037) ([code-elinka](https://github.com/code-elinka))
- Add Observe\(\) func to internal GCERateLimiter [\#2036](https://github.com/kubernetes/ingress-gce/pull/2036) ([spencerhance](https://github.com/spencerhance))
- Replace legacy-cloud-providers/gce with cloud-provider-gcp [\#2033](https://github.com/kubernetes/ingress-gce/pull/2033) ([spencerhance](https://github.com/spencerhance))
- run presubmits with pull-request created image [\#2029](https://github.com/kubernetes/ingress-gce/pull/2029) ([aojea](https://github.com/aojea))
- Flag for changes regarding health checks with BackendConfig [\#2018](https://github.com/kubernetes/ingress-gce/pull/2018) ([DamianSawicki](https://github.com/DamianSawicki))
- Bump k8s-cloud-provider to v1.22.0 [\#1991](https://github.com/kubernetes/ingress-gce/pull/1991) ([spencerhance](https://github.com/spencerhance))

## [v1.22.2](https://github.com/kubernetes/ingress-gce/tree/v1.22.2) (2023-03-20)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.22.1...v1.22.2)

**Merged pull requests:**

- Add code-elinka to the owners list [\#2014](https://github.com/kubernetes/ingress-gce/pull/2014) ([code-elinka](https://github.com/code-elinka))

## [v1.22.1](https://github.com/kubernetes/ingress-gce/tree/v1.22.1) (2023-03-18)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.22.0...v1.22.1)

**Closed issues:**

- \[Action Required\] Update references from k8s.gcr.io to registry.k8s.io [\#1939](https://github.com/kubernetes/ingress-gce/issues/1939)

**Merged pull requests:**

- Add Entry Point for LabelPropagationConfig and change tests related to that [\#2017](https://github.com/kubernetes/ingress-gce/pull/2017) ([songrx1997](https://github.com/songrx1997))
- Process IPv6 Endpoints  [\#2012](https://github.com/kubernetes/ingress-gce/pull/2012) ([swetharepakula](https://github.com/swetharepakula))
- Adjust health check Description when BackendConfig used [\#2008](https://github.com/kubernetes/ingress-gce/pull/2008) ([DamianSawicki](https://github.com/DamianSawicki))
- Add log of processing time for the service metrics. [\#2007](https://github.com/kubernetes/ingress-gce/pull/2007) ([mmamczur](https://github.com/mmamczur))
- Fix PublishL4NetLBDualStackSyncLatency [\#2006](https://github.com/kubernetes/ingress-gce/pull/2006) ([panslava](https://github.com/panslava))
- Add flag enable-label-propagation for label propagation feature [\#1995](https://github.com/kubernetes/ingress-gce/pull/1995) ([songrx1997](https://github.com/songrx1997))
- Update duplicate endpoint handling [\#1994](https://github.com/kubernetes/ingress-gce/pull/1994) ([sawsa307](https://github.com/sawsa307))
- Create the flag for degraded mode [\#1993](https://github.com/kubernetes/ingress-gce/pull/1993) ([sawsa307](https://github.com/sawsa307))
- Fix error caused by unhashable error list [\#1992](https://github.com/kubernetes/ingress-gce/pull/1992) ([sawsa307](https://github.com/sawsa307))
- Replace k8s.gcr.io references with registry.k8s.io [\#1989](https://github.com/kubernetes/ingress-gce/pull/1989) ([asa3311](https://github.com/asa3311))
- Add type PodLabelPropagationConfig structs [\#1988](https://github.com/kubernetes/ingress-gce/pull/1988) ([songrx1997](https://github.com/songrx1997))
- Fix log wrong format argument [\#1986](https://github.com/kubernetes/ingress-gce/pull/1986) ([panslava](https://github.com/panslava))
- Add flag for Transparent Health Check [\#1985](https://github.com/kubernetes/ingress-gce/pull/1985) ([DamianSawicki](https://github.com/DamianSawicki))
- Refactor error state checking [\#1983](https://github.com/kubernetes/ingress-gce/pull/1983) ([sawsa307](https://github.com/sawsa307))
- Revert "Add metrics for sync result" [\#1982](https://github.com/kubernetes/ingress-gce/pull/1982) ([sawsa307](https://github.com/sawsa307))
- Add smaller bucket sizes for workqueue metrics [\#1981](https://github.com/kubernetes/ingress-gce/pull/1981) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Revert "Add metrics for syncer states" [\#1980](https://github.com/kubernetes/ingress-gce/pull/1980) ([sawsa307](https://github.com/sawsa307))
- run e2e tests [\#1978](https://github.com/kubernetes/ingress-gce/pull/1978) ([aojea](https://github.com/aojea))
- Concurrently process deletion candidates within NEG Garbage Collector [\#1976](https://github.com/kubernetes/ingress-gce/pull/1976) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Remove endpoint slices flag [\#1975](https://github.com/kubernetes/ingress-gce/pull/1975) ([sawsa307](https://github.com/sawsa307))
- missing argument to klog [\#1974](https://github.com/kubernetes/ingress-gce/pull/1974) ([aojea](https://github.com/aojea))
- Only use endpoint slices to generate network endpoints [\#1973](https://github.com/kubernetes/ingress-gce/pull/1973) ([swetharepakula](https://github.com/swetharepakula))
- Add names to queues used inside the NEG controller for enabling queue metrics [\#1972](https://github.com/kubernetes/ingress-gce/pull/1972) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Update docker authentication [\#1971](https://github.com/kubernetes/ingress-gce/pull/1971) ([sawsa307](https://github.com/sawsa307))
- bump kubernetes dependencies to 1.26.1 [\#1969](https://github.com/kubernetes/ingress-gce/pull/1969) ([aojea](https://github.com/aojea))
- Edit verbose option [\#1968](https://github.com/kubernetes/ingress-gce/pull/1968) ([sawsa307](https://github.com/sawsa307))
- Replace fetch from apiserver with informer cache while processing SvcNeg [\#1967](https://github.com/kubernetes/ingress-gce/pull/1967) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Introduce rate limiter latency metrics [\#1965](https://github.com/kubernetes/ingress-gce/pull/1965) ([alexkats](https://github.com/alexkats))
- Filter pods from non-existent node in degraded mode [\#1962](https://github.com/kubernetes/ingress-gce/pull/1962) ([sawsa307](https://github.com/sawsa307))
- Add nil check for FrontendConfig in checkProxy\(\) [\#1961](https://github.com/kubernetes/ingress-gce/pull/1961) ([spencerhance](https://github.com/spencerhance))
- Create GKE diagnoser command base and update vendor [\#1960](https://github.com/kubernetes/ingress-gce/pull/1960) ([songrx1997](https://github.com/songrx1997))
- Filter terminal pods in degraded mode [\#1958](https://github.com/kubernetes/ingress-gce/pull/1958) ([sawsa307](https://github.com/sawsa307))
- Create skeleton code for degraded mode procedure [\#1952](https://github.com/kubernetes/ingress-gce/pull/1952) ([sawsa307](https://github.com/sawsa307))
- Export various stats about services in the metrics exported by this c… [\#1943](https://github.com/kubernetes/ingress-gce/pull/1943) ([mmamczur](https://github.com/mmamczur))
- Define endpoint state [\#1934](https://github.com/kubernetes/ingress-gce/pull/1934) ([sawsa307](https://github.com/sawsa307))
- Add metrics for syncer states [\#1912](https://github.com/kubernetes/ingress-gce/pull/1912) ([sawsa307](https://github.com/sawsa307))
- Add metrics for sync result [\#1911](https://github.com/kubernetes/ingress-gce/pull/1911) ([sawsa307](https://github.com/sawsa307))

## [v1.22.0](https://github.com/kubernetes/ingress-gce/tree/v1.22.0) (2023-02-17)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.21.1...v1.22.0)

**Merged pull requests:**

- Fix waitGroup race condition bug [\#1959](https://github.com/kubernetes/ingress-gce/pull/1959) ([sawsa307](https://github.com/sawsa307))
- improve L4FailedHealthCheckCount metric  [\#1955](https://github.com/kubernetes/ingress-gce/pull/1955) ([cezarygerard](https://github.com/cezarygerard))
- Add health status metric for L4 DualStack Controllers [\#1947](https://github.com/kubernetes/ingress-gce/pull/1947) ([panslava](https://github.com/panslava))
- Add L4 DualStack Sync Latency metrics [\#1945](https://github.com/kubernetes/ingress-gce/pull/1945) ([panslava](https://github.com/panslava))
- Add new metric status PersistentError for L4 DualStack [\#1944](https://github.com/kubernetes/ingress-gce/pull/1944) ([panslava](https://github.com/panslava))
- Add L4 NetLB Dual-Stack Metrics [\#1937](https://github.com/kubernetes/ingress-gce/pull/1937) ([panslava](https://github.com/panslava))
- Define sync results [\#1933](https://github.com/kubernetes/ingress-gce/pull/1933) ([sawsa307](https://github.com/sawsa307))
- Refactor syncNetworkEndpoints [\#1931](https://github.com/kubernetes/ingress-gce/pull/1931) ([sawsa307](https://github.com/sawsa307))
- Add metrics to track endpointslice staleness [\#1930](https://github.com/kubernetes/ingress-gce/pull/1930) ([sawsa307](https://github.com/sawsa307))
- Add metrics to track syncer staleness [\#1927](https://github.com/kubernetes/ingress-gce/pull/1927) ([sawsa307](https://github.com/sawsa307))
- Change e2e-test dockerfile [\#1863](https://github.com/kubernetes/ingress-gce/pull/1863) ([code-elinka](https://github.com/code-elinka))

## [v1.21.1](https://github.com/kubernetes/ingress-gce/tree/v1.21.1) (2023-02-15)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.20.3...v1.21.1)

**Merged pull requests:**

- Only process IPv4 Endpoints [\#1951](https://github.com/kubernetes/ingress-gce/pull/1951) ([swetharepakula](https://github.com/swetharepakula))
- configmapcontroller use the informer cache [\#1946](https://github.com/kubernetes/ingress-gce/pull/1946) ([aojea](https://github.com/aojea))
- Initialize metrics collector [\#1935](https://github.com/kubernetes/ingress-gce/pull/1935) ([sawsa307](https://github.com/sawsa307))
- Add Dual-Stack support to L4 NetLB [\#1825](https://github.com/kubernetes/ingress-gce/pull/1825) ([panslava](https://github.com/panslava))

## [v1.20.3](https://github.com/kubernetes/ingress-gce/tree/v1.20.3) (2023-02-13)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.21.0...v1.20.3)

## [v1.21.0](https://github.com/kubernetes/ingress-gce/tree/v1.21.0) (2023-02-10)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.20.2...v1.21.0)

**Implemented enhancements:**

- Support all session affinity types available on the underlying load balancer resource [\#1785](https://github.com/kubernetes/ingress-gce/issues/1785)
- Google SM usage for OauthCredentials in backendendconfig CRD to enable IAP in K8S [\#1770](https://github.com/kubernetes/ingress-gce/issues/1770)
- Health checks should accepts 3XX response codes as healthy [\#1768](https://github.com/kubernetes/ingress-gce/issues/1768)
- Feature: Add labels to forwarding rule being created [\#1753](https://github.com/kubernetes/ingress-gce/issues/1753)
- Feature: Configure target-https-proxies quic [\#1752](https://github.com/kubernetes/ingress-gce/issues/1752)
- Add release note for recent versions [\#1670](https://github.com/kubernetes/ingress-gce/issues/1670)
- How to enable Access-Control-Allow-Origin from gce-ingress [\#1646](https://github.com/kubernetes/ingress-gce/issues/1646)
- BackendConfig support for user-defined response headers [\#1268](https://github.com/kubernetes/ingress-gce/issues/1268)
- Custom response headers support? [\#1106](https://github.com/kubernetes/ingress-gce/issues/1106)

**Closed issues:**

- Using HTTP2 with GKE and Google Managed Certificates [\#1898](https://github.com/kubernetes/ingress-gce/issues/1898)
- Pin dependencies of update-codegen.sh [\#1870](https://github.com/kubernetes/ingress-gce/issues/1870)
- Cannot use the script /hack/update-codegen.sh [\#1855](https://github.com/kubernetes/ingress-gce/issues/1855)
- k8s.gcr.io/defaultbackend-amd64:1.5 has many security vulnerabilities [\#1794](https://github.com/kubernetes/ingress-gce/issues/1794)
- Routing for web sockets [\#1765](https://github.com/kubernetes/ingress-gce/issues/1765)
- Ingress fails to infer health check parameters from readiness check [\#1762](https://github.com/kubernetes/ingress-gce/issues/1762)
- Replace klog.TODO\(\) in the call to neg.NewController\(\) with a top level logger configuration once one becomes available.  [\#1761](https://github.com/kubernetes/ingress-gce/issues/1761)
- use a sslCertificates from another project [\#1682](https://github.com/kubernetes/ingress-gce/issues/1682)

**Merged pull requests:**

- add aojea as approver [\#1940](https://github.com/kubernetes/ingress-gce/pull/1940) ([aojea](https://github.com/aojea))
- Create NEG metrics collector interface [\#1932](https://github.com/kubernetes/ingress-gce/pull/1932) ([sawsa307](https://github.com/sawsa307))
- Change resync period to 10 minutes [\#1929](https://github.com/kubernetes/ingress-gce/pull/1929) ([panslava](https://github.com/panslava))
- Trigger updating DualStack ILB Service if ipFamilies changed [\#1928](https://github.com/kubernetes/ingress-gce/pull/1928) ([panslava](https://github.com/panslava))
- Remove Services.Get\(\) from syncNegStatusAnnotation\(\) [\#1926](https://github.com/kubernetes/ingress-gce/pull/1926) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Update neg endpoint metrics histogram bucket [\#1925](https://github.com/kubernetes/ingress-gce/pull/1925) ([sawsa307](https://github.com/sawsa307))
- Remove support for DestinationRule based subset [\#1921](https://github.com/kubernetes/ingress-gce/pull/1921) ([swetharepakula](https://github.com/swetharepakula))
- Cleanup node port utils GetNodePorts [\#1917](https://github.com/kubernetes/ingress-gce/pull/1917) ([cezarygerard](https://github.com/cezarygerard))
- Inline dependency on legacy-cloud-providers [\#1916](https://github.com/kubernetes/ingress-gce/pull/1916) ([wstcliyu](https://github.com/wstcliyu))
- use bash for verify scripts [\#1915](https://github.com/kubernetes/ingress-gce/pull/1915) ([aojea](https://github.com/aojea))
- Fix code gen process and pin dependencies for update-codegen.sh [\#1914](https://github.com/kubernetes/ingress-gce/pull/1914) ([aojea](https://github.com/aojea))
- Clean up error state checking [\#1909](https://github.com/kubernetes/ingress-gce/pull/1909) ([sawsa307](https://github.com/sawsa307))
- Remove using node ports for L4 NetLB RBS Services [\#1907](https://github.com/kubernetes/ingress-gce/pull/1907) ([panslava](https://github.com/panslava))
- Add delay for retrying NEG polling for unhealthy pods [\#1906](https://github.com/kubernetes/ingress-gce/pull/1906) ([alexkats](https://github.com/alexkats))
- Add UserError Status to ILB DualStack metrics [\#1905](https://github.com/kubernetes/ingress-gce/pull/1905) ([panslava](https://github.com/panslava))
- NEG: Add wrappers for NEG Windows test. [\#1904](https://github.com/kubernetes/ingress-gce/pull/1904) ([shettyg](https://github.com/shettyg))
- Add support for IPv6 values in loadBalancerSourceRanges [\#1903](https://github.com/kubernetes/ingress-gce/pull/1903) ([panslava](https://github.com/panslava))
- Change dual-stack ILB metrics to use snake case labels [\#1901](https://github.com/kubernetes/ingress-gce/pull/1901) ([panslava](https://github.com/panslava))
- Fix logs, use proper format strings argument types [\#1899](https://github.com/kubernetes/ingress-gce/pull/1899) ([panslava](https://github.com/panslava))
- Add IsSubnetworkMissingIPv6GCEError to User Errors [\#1897](https://github.com/kubernetes/ingress-gce/pull/1897) ([panslava](https://github.com/panslava))
- Reimplement getting load balancer source ranges [\#1896](https://github.com/kubernetes/ingress-gce/pull/1896) ([panslava](https://github.com/panslava))
- ILB: Extend the 'number\_of\_l4\_ilbs' metric to track user errors and tre… [\#1895](https://github.com/kubernetes/ingress-gce/pull/1895) ([mmamczur](https://github.com/mmamczur))
- NetLB: Treat LoadBalancer IP setup errors as user errors. [\#1890](https://github.com/kubernetes/ingress-gce/pull/1890) ([mmamczur](https://github.com/mmamczur))
- Revert "Don't log all node names" [\#1888](https://github.com/kubernetes/ingress-gce/pull/1888) ([aojea](https://github.com/aojea))
- don't use alpine for testing [\#1840](https://github.com/kubernetes/ingress-gce/pull/1840) ([aojea](https://github.com/aojea))

## [v1.20.2](https://github.com/kubernetes/ingress-gce/tree/v1.20.2) (2022-12-14)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.20.1...v1.20.2)

**Implemented enhancements:**

- Allow naming NEG when ingress: true [\#1727](https://github.com/kubernetes/ingress-gce/issues/1727)

**Closed issues:**

- Do we have header bases routing in ingress-gce controller? [\#1860](https://github.com/kubernetes/ingress-gce/issues/1860)
- Forwarding rule's subnetwork must have purpose=PRIVATE [\#1852](https://github.com/kubernetes/ingress-gce/issues/1852)
- I think NEG finalizers are making my namespaces take 10+ mins to delete [\#1720](https://github.com/kubernetes/ingress-gce/issues/1720)
- Support for GRPC Service name wildcards [\#1684](https://github.com/kubernetes/ingress-gce/issues/1684)
- How to properly redirect www.\* to non www [\#1681](https://github.com/kubernetes/ingress-gce/issues/1681)
- GCE load balancer health check does match k8s pod health [\#1656](https://github.com/kubernetes/ingress-gce/issues/1656)

**Merged pull requests:**

- Fix code gen process and pin dependencies for update-codegen.sh [\#1884](https://github.com/kubernetes/ingress-gce/pull/1884) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add missing string params to ensureNodesFirewall logs [\#1882](https://github.com/kubernetes/ingress-gce/pull/1882) ([panslava](https://github.com/panslava))
- NetLB controller: cleanup RBS resources in case service type changed [\#1881](https://github.com/kubernetes/ingress-gce/pull/1881) ([cezarygerard](https://github.com/cezarygerard))
- Change e2e regional address to use full subnet path [\#1880](https://github.com/kubernetes/ingress-gce/pull/1880) ([spencerhance](https://github.com/spencerhance))
- Fix L4 NetLB RBS cleanup when Service Type changed [\#1879](https://github.com/kubernetes/ingress-gce/pull/1879) ([panslava](https://github.com/panslava))
- Fixes subnet in e2e test command [\#1871](https://github.com/kubernetes/ingress-gce/pull/1871) ([spencerhance](https://github.com/spencerhance))
- Adds Subnet field to e2e-test [\#1867](https://github.com/kubernetes/ingress-gce/pull/1867) ([spencerhance](https://github.com/spencerhance))
- add ojea as reviewer [\#1862](https://github.com/kubernetes/ingress-gce/pull/1862) ([aojea](https://github.com/aojea))
- Update e2e.EnsureIngress\(\) to check Annotations in addition to Spec [\#1859](https://github.com/kubernetes/ingress-gce/pull/1859) ([spencerhance](https://github.com/spencerhance))
- wait on startup to get permissions [\#1858](https://github.com/kubernetes/ingress-gce/pull/1858) ([aojea](https://github.com/aojea))
- Bump k8s-cloud-provider to v1.20.0 [\#1857](https://github.com/kubernetes/ingress-gce/pull/1857) ([spencerhance](https://github.com/spencerhance))
- Add L4 ILB Dual-Stack Metrics [\#1856](https://github.com/kubernetes/ingress-gce/pull/1856) ([panslava](https://github.com/panslava))
- Add check for attach/detach API error [\#1853](https://github.com/kubernetes/ingress-gce/pull/1853) ([sawsa307](https://github.com/sawsa307))
- Add more logs to L4 controllers [\#1850](https://github.com/kubernetes/ingress-gce/pull/1850) ([panslava](https://github.com/panslava))
- Add check for endpoint count is zero [\#1849](https://github.com/kubernetes/ingress-gce/pull/1849) ([sawsa307](https://github.com/sawsa307))
- Remove emitting Update event on top-level in L4 NetLB [\#1847](https://github.com/kubernetes/ingress-gce/pull/1847) ([panslava](https://github.com/panslava))
- Add check for invalid or missing data in EPS [\#1842](https://github.com/kubernetes/ingress-gce/pull/1842) ([sawsa307](https://github.com/sawsa307))
- Add check for endpoint counts equal [\#1836](https://github.com/kubernetes/ingress-gce/pull/1836) ([sawsa307](https://github.com/sawsa307))
- Create Error State Interface [\#1835](https://github.com/kubernetes/ingress-gce/pull/1835) ([sawsa307](https://github.com/sawsa307))
- Refactor instances package, move node controller out of l7 [\#1826](https://github.com/kubernetes/ingress-gce/pull/1826) ([panslava](https://github.com/panslava))
- Spelling [\#1819](https://github.com/kubernetes/ingress-gce/pull/1819) ([jsoref](https://github.com/jsoref))

## [v1.20.1](https://github.com/kubernetes/ingress-gce/tree/v1.20.1) (2022-10-26)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.20.0...v1.20.1)

**Implemented enhancements:**

- Feature: Support custom TLS certificates when using HTTPS to the backends [\#1305](https://github.com/kubernetes/ingress-gce/issues/1305)

**Closed issues:**

- Is it possible to health check on a port not published as a service port?  [\#1680](https://github.com/kubernetes/ingress-gce/issues/1680)

**Merged pull requests:**

- Add more logs to L4 NetLB [\#1845](https://github.com/kubernetes/ingress-gce/pull/1845) ([panslava](https://github.com/panslava))
- Add statusCodeHandler to 404-server-with-metrics which serves status code given in URL path [\#1844](https://github.com/kubernetes/ingress-gce/pull/1844) ([yiyangy](https://github.com/yiyangy))
- Reserve ILB IP address before deleting FR on protocol change [\#1839](https://github.com/kubernetes/ingress-gce/pull/1839) ([panslava](https://github.com/panslava))
- Add Dual-Stack support to L4 ILB [\#1782](https://github.com/kubernetes/ingress-gce/pull/1782) ([panslava](https://github.com/panslava))

## [v1.20.0](https://github.com/kubernetes/ingress-gce/tree/v1.20.0) (2022-10-15)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.9...v1.20.0)

**Merged pull requests:**

- Fix ports read/write races in neg tests [\#1837](https://github.com/kubernetes/ingress-gce/pull/1837) ([panslava](https://github.com/panslava))
- increase test coverage [\#1833](https://github.com/kubernetes/ingress-gce/pull/1833) ([aojea](https://github.com/aojea))

## [v1.14.9](https://github.com/kubernetes/ingress-gce/tree/v1.14.9) (2022-10-13)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.19.2...v1.14.9)

**Fixed bugs:**

- Applying backend-config annotation on existing ingress service has no effect [\#1503](https://github.com/kubernetes/ingress-gce/issues/1503)

**Closed issues:**

- Allow custom name for http\(s\) load balancing [\#1832](https://github.com/kubernetes/ingress-gce/issues/1832)
- Possible Regression: BackendConfig timeoutSec doesn't seem to work with GCE Ingress [\#1803](https://github.com/kubernetes/ingress-gce/issues/1803)
- Migrate to LeasesResourceLock from ConfigMapsLeases [\#1713](https://github.com/kubernetes/ingress-gce/issues/1713)
- Support for URL redirects [\#1677](https://github.com/kubernetes/ingress-gce/issues/1677)

**Merged pull requests:**

- Add more buckets to L4 latency metrics [\#1830](https://github.com/kubernetes/ingress-gce/pull/1830) ([cezarygerard](https://github.com/cezarygerard))
- Delete L4LB healthcheck test [\#1829](https://github.com/kubernetes/ingress-gce/pull/1829) ([code-elinka](https://github.com/code-elinka))
- Change l4 healthcheck timeout [\#1828](https://github.com/kubernetes/ingress-gce/pull/1828) ([code-elinka](https://github.com/code-elinka))
- Add a new NetL4LB flag [\#1824](https://github.com/kubernetes/ingress-gce/pull/1824) ([code-elinka](https://github.com/code-elinka))
- Refactor l4 tests [\#1822](https://github.com/kubernetes/ingress-gce/pull/1822) ([panslava](https://github.com/panslava))
- Use namer.L4Firewall and namer.L4Backend instead of servicePort [\#1821](https://github.com/kubernetes/ingress-gce/pull/1821) ([panslava](https://github.com/panslava))
- Create struct argument L4NetLBParams for L4NetLB constructor [\#1820](https://github.com/kubernetes/ingress-gce/pull/1820) ([panslava](https://github.com/panslava))
- Fix L4 Premium Network Tier metric, fix comments [\#1818](https://github.com/kubernetes/ingress-gce/pull/1818) ([panslava](https://github.com/panslava))
- Small refactor of l4controller\_test assertions  [\#1817](https://github.com/kubernetes/ingress-gce/pull/1817) ([panslava](https://github.com/panslava))
- Log if health check intended shared on deletion [\#1815](https://github.com/kubernetes/ingress-gce/pull/1815) ([panslava](https://github.com/panslava))
- Fix deletion of address in l4 [\#1814](https://github.com/kubernetes/ingress-gce/pull/1814) ([panslava](https://github.com/panslava))
- Fix errcheck linter for all non-test files [\#1813](https://github.com/kubernetes/ingress-gce/pull/1813) ([panslava](https://github.com/panslava))
- Change healthchecksl4 sync mechanism [\#1812](https://github.com/kubernetes/ingress-gce/pull/1812) ([panslava](https://github.com/panslava))
- Remove go test command with -i flag, cause it was deprecated [\#1811](https://github.com/kubernetes/ingress-gce/pull/1811) ([panslava](https://github.com/panslava))
- Add checking for firewall source ranges in l4\_test [\#1810](https://github.com/kubernetes/ingress-gce/pull/1810) ([panslava](https://github.com/panslava))
- remove rramkumar1 from owners [\#1809](https://github.com/kubernetes/ingress-gce/pull/1809) ([rramkumar1](https://github.com/rramkumar1))
- Refactor healthchecksl4, change functions order, rename functions [\#1808](https://github.com/kubernetes/ingress-gce/pull/1808) ([panslava](https://github.com/panslava))
- Add Ingress Support for L7-ILB HTTP+HTTPS on same LB [\#1807](https://github.com/kubernetes/ingress-gce/pull/1807) ([sawsa307](https://github.com/sawsa307))
- Add L4Firewall to namer, instead of using L4Backend [\#1806](https://github.com/kubernetes/ingress-gce/pull/1806) ([panslava](https://github.com/panslava))
- Refactor l4\_test assertInternalLbResourcesDeleted function [\#1805](https://github.com/kubernetes/ingress-gce/pull/1805) ([panslava](https://github.com/panslava))
- Refactor l4\_test assertion function [\#1804](https://github.com/kubernetes/ingress-gce/pull/1804) ([panslava](https://github.com/panslava))
- Reformat forwarding rules provider [\#1802](https://github.com/kubernetes/ingress-gce/pull/1802) ([panslava](https://github.com/panslava))
- fix typo in instruction [\#1801](https://github.com/kubernetes/ingress-gce/pull/1801) ([sawsa307](https://github.com/sawsa307))
- Fix deleting address in RBS [\#1800](https://github.com/kubernetes/ingress-gce/pull/1800) ([panslava](https://github.com/panslava))
- Remove duplicate code [\#1799](https://github.com/kubernetes/ingress-gce/pull/1799) ([panslava](https://github.com/panslava))
- Fix typo NewL4SynResult -\> NewL4SyncResult [\#1798](https://github.com/kubernetes/ingress-gce/pull/1798) ([panslava](https://github.com/panslava))
- Use ForwardingRulesProvider, instead of composite.Delete, on deleting L4 Forwarding Rules [\#1797](https://github.com/kubernetes/ingress-gce/pull/1797) ([panslava](https://github.com/panslava))
- Update e2e-test README [\#1795](https://github.com/kubernetes/ingress-gce/pull/1795) ([spencerhance](https://github.com/spencerhance))
- Cleanup code [\#1791](https://github.com/kubernetes/ingress-gce/pull/1791) ([cezarygerard](https://github.com/cezarygerard))
- Refactor L4 Namer, add getSuffixedName, getTrimmedNamespacedName function [\#1790](https://github.com/kubernetes/ingress-gce/pull/1790) ([panslava](https://github.com/panslava))
- Rename \*L4 method receivers from l -\> l4 [\#1789](https://github.com/kubernetes/ingress-gce/pull/1789) ([panslava](https://github.com/panslava))
- Create getSubnetworkURLByName to refactor part of the L4 code [\#1788](https://github.com/kubernetes/ingress-gce/pull/1788) ([panslava](https://github.com/panslava))
- Extract NewL4 arguments into params struct [\#1787](https://github.com/kubernetes/ingress-gce/pull/1787) ([panslava](https://github.com/panslava))
- Extract to separate function L4HealthCheckFirewall from L4 namer L4HealthCheck [\#1786](https://github.com/kubernetes/ingress-gce/pull/1786) ([panslava](https://github.com/panslava))
- Update Go to version 1.18. Run go mod tide, gofmt [\#1784](https://github.com/kubernetes/ingress-gce/pull/1784) ([panslava](https://github.com/panslava))
- cleanup l4 tests [\#1781](https://github.com/kubernetes/ingress-gce/pull/1781) ([cezarygerard](https://github.com/cezarygerard))
- ConfigMap informer should only watch --asm-configmap-based-config-nam… [\#1780](https://github.com/kubernetes/ingress-gce/pull/1780) ([bowei](https://github.com/bowei))
- Increase timeouts to reflect GCE API [\#1777](https://github.com/kubernetes/ingress-gce/pull/1777) ([swetharepakula](https://github.com/swetharepakula))
- Add Custom response header e2e test [\#1776](https://github.com/kubernetes/ingress-gce/pull/1776) ([songrx1997](https://github.com/songrx1997))
- Add Custom Response Header Logic and the Unit Tests for it [\#1772](https://github.com/kubernetes/ingress-gce/pull/1772) ([songrx1997](https://github.com/songrx1997))
- Add Changes for Custom Response Header API [\#1771](https://github.com/kubernetes/ingress-gce/pull/1771) ([songrx1997](https://github.com/songrx1997))
- Add golang-ci with errcheck enabled to test suite to ensure all errors produced are checked [\#1769](https://github.com/kubernetes/ingress-gce/pull/1769) ([michaelasp](https://github.com/michaelasp))
- Split GetPortsAndProtocol function to 4 separate functions [\#1764](https://github.com/kubernetes/ingress-gce/pull/1764) ([panslava](https://github.com/panslava))
- Create Health Checks Provider to interact with Google Cloud [\#1763](https://github.com/kubernetes/ingress-gce/pull/1763) ([panslava](https://github.com/panslava))
- Create Forwarding Rules provider [\#1759](https://github.com/kubernetes/ingress-gce/pull/1759) ([panslava](https://github.com/panslava))
- Fix creation of NEGs in the new zone when cluster spans to the new zone [\#1754](https://github.com/kubernetes/ingress-gce/pull/1754) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Migrate gce-ingress to LeasesResourceLock from ConfigMapsLeases [\#1750](https://github.com/kubernetes/ingress-gce/pull/1750) ([songrx1997](https://github.com/songrx1997))
- updated changelog [\#1701](https://github.com/kubernetes/ingress-gce/pull/1701) ([kundan2707](https://github.com/kundan2707))

## [v1.19.2](https://github.com/kubernetes/ingress-gce/tree/v1.19.2) (2022-08-12)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.19.1...v1.19.2)

**Fixed bugs:**

- BackendConfig.securityPolicy is not removed after config updates [\#1706](https://github.com/kubernetes/ingress-gce/issues/1706)

**Merged pull requests:**

- Fix BackendConfig.securityPolicy is not removed after config updates [\#1749](https://github.com/kubernetes/ingress-gce/pull/1749) ([songrx1997](https://github.com/songrx1997))

## [v1.19.1](https://github.com/kubernetes/ingress-gce/tree/v1.19.1) (2022-08-08)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.19.0...v1.19.1)

**Closed issues:**

- Zombie ingress keeps being created after renaming and manual deletion [\#1767](https://github.com/kubernetes/ingress-gce/issues/1767)
- Ingress not polling/syncing with Backendconfig CRD  [\#1744](https://github.com/kubernetes/ingress-gce/issues/1744)

**Merged pull requests:**

- Fix RBS crash, when getting NodePorts for Services without ports [\#1774](https://github.com/kubernetes/ingress-gce/pull/1774) ([panslava](https://github.com/panslava))
- Update the generated code [\#1760](https://github.com/kubernetes/ingress-gce/pull/1760) ([songrx1997](https://github.com/songrx1997))
- Fix DestinationRanges update when IP changes [\#1748](https://github.com/kubernetes/ingress-gce/pull/1748) ([sugangli](https://github.com/sugangli))
- Add minor logs to track processing track for neg controller [\#1747](https://github.com/kubernetes/ingress-gce/pull/1747) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Configure contextual and structured logging for NEG controller [\#1746](https://github.com/kubernetes/ingress-gce/pull/1746) ([gauravkghildiyal](https://github.com/gauravkghildiyal))
- Add maxIGSize to NodePool constructor, instead of using global flag [\#1715](https://github.com/kubernetes/ingress-gce/pull/1715) ([panslava](https://github.com/panslava))

## [v1.19.0](https://github.com/kubernetes/ingress-gce/tree/v1.19.0) (2022-06-28)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.15.3...v1.19.0)

**Merged pull requests:**

- Delete RBS annotations as a last step on garbage collection [\#1743](https://github.com/kubernetes/ingress-gce/pull/1743) ([panslava](https://github.com/panslava))
- Handle L4 ELB RBS controller race with legacy on service creation [\#1742](https://github.com/kubernetes/ingress-gce/pull/1742) ([panslava](https://github.com/panslava))
- Fix preventTargetPoolToRBSMigration. Properly delete annotation [\#1741](https://github.com/kubernetes/ingress-gce/pull/1741) ([panslava](https://github.com/panslava))
- Prevent migration to RBS for legacy Target pool services. Remove RBS annotation for such services [\#1740](https://github.com/kubernetes/ingress-gce/pull/1740) ([panslava](https://github.com/panslava))
- Extend transition timeout to 15 min [\#1739](https://github.com/kubernetes/ingress-gce/pull/1739) ([swetharepakula](https://github.com/swetharepakula))
- Fix firewall pinhole [\#1674](https://github.com/kubernetes/ingress-gce/pull/1674) ([sugangli](https://github.com/sugangli))

## [v1.15.3](https://github.com/kubernetes/ingress-gce/tree/v1.15.3) (2022-06-21)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.8...v1.15.3)

## [v1.14.8](https://github.com/kubernetes/ingress-gce/tree/v1.14.8) (2022-06-21)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.18.0...v1.14.8)

## [v1.18.0](https://github.com/kubernetes/ingress-gce/tree/v1.18.0) (2022-06-17)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.17.1...v1.18.0)

**Implemented enhancements:**

- Link to gke-self-managed.sh broken [\#1702](https://github.com/kubernetes/ingress-gce/issues/1702)

**Fixed bugs:**

- Ingress controller crash if endpoint slice missing service label [\#1730](https://github.com/kubernetes/ingress-gce/issues/1730)
- Link to gke-self-managed.sh broken [\#1702](https://github.com/kubernetes/ingress-gce/issues/1702)

**Closed issues:**

- K8S Ingress spec missing URL rewrite capability [\#1723](https://github.com/kubernetes/ingress-gce/issues/1723)

**Merged pull requests:**

- Do not return error if EndpointSlicesServiceKey func errors [\#1733](https://github.com/kubernetes/ingress-gce/pull/1733) ([swetharepakula](https://github.com/swetharepakula))
- corrected link for gke-self-managed.sh [\#1703](https://github.com/kubernetes/ingress-gce/pull/1703) ([kundan2707](https://github.com/kundan2707))

## [v1.17.1](https://github.com/kubernetes/ingress-gce/tree/v1.17.1) (2022-06-15)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.17.0...v1.17.1)

**Implemented enhancements:**

- Enable GCE load balancer healthcheck logging [\#1665](https://github.com/kubernetes/ingress-gce/issues/1665)
- Allow more than 50 paths routes per domain [\#837](https://github.com/kubernetes/ingress-gce/issues/837)
- BackendConfig support for balancingMode [\#740](https://github.com/kubernetes/ingress-gce/issues/740)
- How do I set up RPS limit [\#670](https://github.com/kubernetes/ingress-gce/issues/670)
- GCP Description field is blank [\#661](https://github.com/kubernetes/ingress-gce/issues/661)
- Possible to enable OCSP stapling? [\#564](https://github.com/kubernetes/ingress-gce/issues/564)
- Control backend service IAM policy from BackendConfig [\#511](https://github.com/kubernetes/ingress-gce/issues/511)
- Any plan to support QUIC in Google LB ? [\#510](https://github.com/kubernetes/ingress-gce/issues/510)
- Feature Request: Name GCP resources better [\#397](https://github.com/kubernetes/ingress-gce/issues/397)
- Option to share LB between Ingresses [\#369](https://github.com/kubernetes/ingress-gce/issues/369)
- Option to launch GCP TCP Proxy LB from Kubernetes [\#320](https://github.com/kubernetes/ingress-gce/issues/320)
- Support SSL policies [\#246](https://github.com/kubernetes/ingress-gce/issues/246)
- Support multiple addresses \(including IPv6\) [\#87](https://github.com/kubernetes/ingress-gce/issues/87)
- Use GCE load balancer controller with backend buckets [\#33](https://github.com/kubernetes/ingress-gce/issues/33)

**Fixed bugs:**

- Sync error when ingress name has dots [\#614](https://github.com/kubernetes/ingress-gce/issues/614)
- ingress-gce fails to create backend services when across zones with different balancing modes [\#480](https://github.com/kubernetes/ingress-gce/issues/480)
- Disabling TLS on an active ingress needs to delete resources\(target proxy, certs\) [\#465](https://github.com/kubernetes/ingress-gce/issues/465)
- GCE: Converting ephemeral IP to static doesn't update related TargetHTTPSProxy [\#259](https://github.com/kubernetes/ingress-gce/issues/259)

**Closed issues:**

- Loadbalancer controller throws `Error 400: Invalid value for field 'zone': ''. ` [\#1633](https://github.com/kubernetes/ingress-gce/issues/1633)
- Split CRD creation out of GLBC  [\#744](https://github.com/kubernetes/ingress-gce/issues/744)
- Better UX for user who have HttpLoadBalancing add-on disabled [\#645](https://github.com/kubernetes/ingress-gce/issues/645)
- rewrite [\#109](https://github.com/kubernetes/ingress-gce/issues/109)

**Merged pull requests:**

- Use discovery/v1 EndpointSlice [\#1729](https://github.com/kubernetes/ingress-gce/pull/1729) ([swetharepakula](https://github.com/swetharepakula))
- Init first sync error time with transation start time [\#1728](https://github.com/kubernetes/ingress-gce/pull/1728) ([kl52752](https://github.com/kl52752))
- Add new members to OWNERS file [\#1726](https://github.com/kubernetes/ingress-gce/pull/1726) ([kl52752](https://github.com/kl52752))
- Add backend service name into affinity check error [\#1725](https://github.com/kubernetes/ingress-gce/pull/1725) ([kl52752](https://github.com/kl52752))
- Fix last error logging in affinity tests [\#1724](https://github.com/kubernetes/ingress-gce/pull/1724) ([kl52752](https://github.com/kl52752))
- Update api to 0.22.2 [\#1710](https://github.com/kubernetes/ingress-gce/pull/1710) ([sugangli](https://github.com/sugangli))

## [v1.17.0](https://github.com/kubernetes/ingress-gce/tree/v1.17.0) (2022-06-02)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.16.1...v1.17.0)

**Implemented enhancements:**

- Not possible to have client certificate authentication [\#1622](https://github.com/kubernetes/ingress-gce/issues/1622)
- HTTP to HTTPS redirection [\#1075](https://github.com/kubernetes/ingress-gce/issues/1075)
- Consider Emitting Events for GC errors [\#143](https://github.com/kubernetes/ingress-gce/issues/143)
- Ingress Healthcheck Configuration [\#42](https://github.com/kubernetes/ingress-gce/issues/42)

**Fixed bugs:**

- Zonelister logic prevents instance removal from instance group & instance group GC [\#50](https://github.com/kubernetes/ingress-gce/issues/50)
- \[GLBC\] LB garbage collection orphans named ports in instance groups [\#43](https://github.com/kubernetes/ingress-gce/issues/43)
- GCE: ingress only shows the first backend's healthiness in `backends` annotation [\#35](https://github.com/kubernetes/ingress-gce/issues/35)

**Closed issues:**

- Extremely slow ingress reconciliation on path and backend changes \[1.22.8-gke.200\] [\#1700](https://github.com/kubernetes/ingress-gce/issues/1700)
- \[GKE\] How to diagnose backend status to be Unknown [\#1601](https://github.com/kubernetes/ingress-gce/issues/1601)
- Zonal NEG being removed from Load Balancer [\#1589](https://github.com/kubernetes/ingress-gce/issues/1589)
- Adding ingress rule to ingress definition doesn't update Load Balancer definition [\#1546](https://github.com/kubernetes/ingress-gce/issues/1546)
- Update to use Ingress and IngressClass networking.k8s.io/v1 APIs before v1.22 [\#1301](https://github.com/kubernetes/ingress-gce/issues/1301)
- \[GLBC\] Garbage collection runs too frequently - after every sync [\#40](https://github.com/kubernetes/ingress-gce/issues/40)

**Merged pull requests:**

- Add buckets to gce\_api\_request\_duration\_seconds metric [\#1712](https://github.com/kubernetes/ingress-gce/pull/1712) ([cezarygerard](https://github.com/cezarygerard))
- Fix HttpsRedirects Lifecycle [\#1711](https://github.com/kubernetes/ingress-gce/pull/1711) ([spencerhance](https://github.com/spencerhance))
- Fix for backend service update [\#1709](https://github.com/kubernetes/ingress-gce/pull/1709) ([kl52752](https://github.com/kl52752))
- Truncate nodes list to maximum instance group size \(1000\) before adding to instance group [\#1707](https://github.com/kubernetes/ingress-gce/pull/1707) ([panslava](https://github.com/panslava))
- Rewrite L4 healthchecks creation and deletion [\#1705](https://github.com/kubernetes/ingress-gce/pull/1705) ([cezarygerard](https://github.com/cezarygerard))
- corrected link for cluster-setup.md [\#1704](https://github.com/kubernetes/ingress-gce/pull/1704) ([kundan2707](https://github.com/kundan2707))
- Modify reporting error metrics from L4 RBS services -  only report service in error if resync deadline elapsed [\#1699](https://github.com/kubernetes/ingress-gce/pull/1699) ([kl52752](https://github.com/kl52752))
- Do not restart controller after L4 healthcheck failure, publish metrics instead [\#1694](https://github.com/kubernetes/ingress-gce/pull/1694) ([cezarygerard](https://github.com/cezarygerard))
- Add new flag for configuring metrics export frequency [\#1693](https://github.com/kubernetes/ingress-gce/pull/1693) ([cezarygerard](https://github.com/cezarygerard))
- Improve RBS service deletion [\#1691](https://github.com/kubernetes/ingress-gce/pull/1691) ([cezarygerard](https://github.com/cezarygerard))
- Review fixes [\#1685](https://github.com/kubernetes/ingress-gce/pull/1685) ([kl52752](https://github.com/kl52752))

## [v1.16.1](https://github.com/kubernetes/ingress-gce/tree/v1.16.1) (2022-04-05)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.16.0...v1.16.1)

**Merged pull requests:**

- Fix ASM test, use skip namespace for skip service [\#1689](https://github.com/kubernetes/ingress-gce/pull/1689) ([panslava](https://github.com/panslava))
- Delete service and wait for NEG deletion in ASM e2e tests [\#1688](https://github.com/kubernetes/ingress-gce/pull/1688) ([panslava](https://github.com/panslava))
- Upgrade golang version to 1.16 [\#1687](https://github.com/kubernetes/ingress-gce/pull/1687) ([freehan](https://github.com/freehan))
- Make L4 NetLB Controller Healthcheck return error [\#1683](https://github.com/kubernetes/ingress-gce/pull/1683) ([cezarygerard](https://github.com/cezarygerard))
- Remove debug print statement [\#1676](https://github.com/kubernetes/ingress-gce/pull/1676) ([swetharepakula](https://github.com/swetharepakula))

# Changelog


## [v1.16.0](https://github.com/kubernetes/ingress-gce/tree/v1.16.0) (2022-03-25)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.15.2...v1.16.0)
## [v1.15.2](https://github.com/kubernetes/ingress-gce/tree/v1.15.2) (2022-01-25)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.15.1...v1.15.2)
## [v1.14.7](https://github.com/kubernetes/ingress-gce/tree/v1.14.7) (2022-01-29)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.6...v1.14.7)
## [v1.15.1](https://github.com/kubernetes/ingress-gce/tree/v1.15.1) (2022-01-14)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.15.0...v1.15.1)
## [v1.14.6](https://github.com/kubernetes/ingress-gce/tree/v1.14.6) (2022-01-14)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.5...v1.14.6)
## [v1.15.0](https://github.com/kubernetes/ingress-gce/tree/v1.15.0) (2022-01-13)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.7...v1.15.0)
## [v1.14.5](https://github.com/kubernetes/ingress-gce/tree/v1.14.5) (2022-01-05)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.4...v1.14.5)
## [v1.14.4](https://github.com/kubernetes/ingress-gce/tree/v1.14.4) (2021-12-24)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.3...v1.14.4)
## [v1.13.11](https://github.com/kubernetes/ingress-gce/tree/v1.13.11) (2021-12-24)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.10...v1.13.11)
## [v1.14.3](https://github.com/kubernetes/ingress-gce/tree/v1.14.3) (2021-11-24)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.2...v1.14.3)
## [v1.14.2](https://github.com/kubernetes/ingress-gce/tree/v1.14.2) (2021-11-16)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.1...v1.14.2)
## [v1.13.10](https://github.com/kubernetes/ingress-gce/tree/v1.13.10) (2021-11-16)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.9...v1.13.10)
## [v1.12.7](https://github.com/kubernetes/ingress-gce/tree/v1.12.7) (2021-11-16)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.12.6...v1.12.7)
## [v1.11.10](https://github.com/kubernetes/ingress-gce/tree/v1.11.10) (2021-11-16)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.9...v1.11.10)
## [v1.14.1](https://github.com/kubernetes/ingress-gce/tree/v1.14.1) (2021-10-28)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.14.0...v1.14.1)
## [v1.13.9](https://github.com/kubernetes/ingress-gce/tree/v1.13.9) (2021-10-28)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.8...v1.13.9)
## [v1.11.9](https://github.com/kubernetes/ingress-gce/tree/v1.11.9) (2021-10-28)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.8...v1.11.9)
## [v1.11.8](https://github.com/kubernetes/ingress-gce/tree/v1.11.8) (2021-10-02)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.7...v1.11.8)
## [v1.11.7](https://github.com/kubernetes/ingress-gce/tree/v1.11.7) (2021-09-30)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.6...v1.11.7)
## [v1.14.0](https://github.com/kubernetes/ingress-gce/tree/v1.14.0) (2021-09-25)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.11...v1.14.0)
## [v1.13.8](https://github.com/kubernetes/ingress-gce/tree/v1.13.8) (2021-09-17)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.7...v1.13.8)
## [v1.13.7](https://github.com/kubernetes/ingress-gce/tree/v1.13.7) (2021-09-01)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.6...v1.13.7)
## [v1.13.6](https://github.com/kubernetes/ingress-gce/tree/v1.13.6) (2021-08-16)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.5...v1.13.6)
## [v1.13.5](https://github.com/kubernetes/ingress-gce/tree/v1.13.5) (2021-08-14)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.4...v1.13.5)
## [v1.13.4](https://github.com/kubernetes/ingress-gce/tree/v1.13.4) (2021-01-06)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.3...v1.13.4)
## [v1.13.3](https://github.com/kubernetes/ingress-gce/tree/v1.13.3) (2021-07-31)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.2...v1.13.3)
## [v1.13.2](https://github.com/kubernetes/ingress-gce/tree/v1.13.2) (2021-07-30)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.1...v1.13.2)
## [v1.12.6](https://github.com/kubernetes/ingress-gce/tree/v1.12.6) (2021-07-30)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.12.4...v1.12.6)
## [v1.13.1](https://github.com/kubernetes/ingress-gce/tree/v1.13.1) (2021-07-23)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.13.0...v1.13.1)
## [v1.11.5](https://github.com/kubernetes/ingress-gce/tree/v1.11.5) (2021-07-23)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.4...v1.11.5)
## [v1.13.0](https://github.com/kubernetes/ingress-gce/tree/v1.13.0) (2021-07-02)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.12.7...v1.13.0)
## [v1.11.4](https://github.com/kubernetes/ingress-gce/tree/v1.11.4) (2021-06-25)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.3...v1.11.4)
## [v1.11.3](https://github.com/kubernetes/ingress-gce/tree/v1.11.3) (2021-05-28)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.2...v1.11.3)
## [v1.11.2](https://github.com/kubernetes/ingress-gce/tree/v1.11.2) (2021-05-15)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.1...v1.11.2)
## [v1.12.0](https://github.com/kubernetes/ingress-gce/tree/v1.12.0) (2021-05-07)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.10...v1.12.0)
## [v1.11.1](https://github.com/kubernetes/ingress-gce/tree/v1.11.1) (2021-04-21)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.11.0...v1.11.1)
## [v1.10.15](https://github.com/kubernetes/ingress-gce/tree/v1.10.15) (2021-04-02)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.14...v1.10.15)
## [v1.10.14](https://github.com/kubernetes/ingress-gce/tree/v1.10.14) (2021-01-22)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.13...v1.10.14)
## [v1.10.13](https://github.com/kubernetes/ingress-gce/tree/v1.10.13) (2020-12-18)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.12...v1.10.13)
## [v1.10.12](https://github.com/kubernetes/ingress-gce/tree/v1.10.12) (2020-12-18)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.11...v1.10.12)
## [v1.10.11](https://github.com/kubernetes/ingress-gce/tree/v1.10.11) (2020-11-20)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.10...v1.10.11)
## [v1.10.9](https://github.com/kubernetes/ingress-gce/tree/v1.10.9) (2020-10-31)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.8...v1.10.9)
## [v1.10.8](https://github.com/kubernetes/ingress-gce/tree/v1.10.8) (2020-10-29)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.7...v1.10.8)
## [v1.10.7](https://github.com/kubernetes/ingress-gce/tree/v1.10.7) (2020-10-15)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.6...v1.10.7)
## [v1.9.10](https://github.com/kubernetes/ingress-gce/tree/v1.9.10) (2020-10-15)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.9...v1.9.10)
## [v1.10.6](https://github.com/kubernetes/ingress-gce/tree/v1.10.6) (2020-10-08)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.5...v1.10.6)
## [v1.10.5](https://github.com/kubernetes/ingress-gce/tree/v1.10.5) (2020-09-26)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.4...v1.10.5)
## [v1.10.4](https://github.com/kubernetes/ingress-gce/tree/v1.10.4) (2020-09-15)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.3...v1.10.4)
## [v1.10.3](https://github.com/kubernetes/ingress-gce/tree/v1.10.3) (2020-09-04)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.2...v1.10.3)

**Closed issues:**

- Internal Ingress stuck at creating stage [\#1189](https://github.com/kubernetes/ingress-gce/issues/1189)

**Merged pull requests:**

- Cherry Pick \#1246 \[Require id cherry pick\] [\#1247](https://github.com/kubernetes/ingress-gce/pull/1247) ([swetharepakula](https://github.com/swetharepakula))

## [v1.10.2](https://github.com/kubernetes/ingress-gce/tree/v1.10.2) (2020-09-01)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.9...v1.10.2)

**Merged pull requests:**

- Require id in NegObjectReference [\#1246](https://github.com/kubernetes/ingress-gce/pull/1246) ([swetharepakula](https://github.com/swetharepakula))
- Cherry-pick \#1227 \[Update ensureRedirectUrlMap\(\) logic\] into release-1.10 [\#1245](https://github.com/kubernetes/ingress-gce/pull/1245) ([spencerhance](https://github.com/spencerhance))
- cherrypick PR 1236\(resource annotations for ILB\) into release-1.10 [\#1244](https://github.com/kubernetes/ingress-gce/pull/1244) ([prameshj](https://github.com/prameshj))
- Cherrypick PR 1241\(Lookup forwarding rule before deletion on protocol change\) into release-1.10 [\#1243](https://github.com/kubernetes/ingress-gce/pull/1243) ([prameshj](https://github.com/prameshj))
- Cherry-pick \#1239 \[Add HTTPS Redirects to Metrics\] into release-1-10 [\#1242](https://github.com/kubernetes/ingress-gce/pull/1242) ([spencerhance](https://github.com/spencerhance))
- Cherry-pick \#1233 \[Add missing features to ingress metrics\] into release-1-10 [\#1238](https://github.com/kubernetes/ingress-gce/pull/1238) ([spencerhance](https://github.com/spencerhance))
- Cherry Pick \#1230 \[Enable neg crd by default\] [\#1235](https://github.com/kubernetes/ingress-gce/pull/1235) ([swetharepakula](https://github.com/swetharepakula))
- Cherry-pick \#1223 \[Fix internal ingress static IP bug\] into release-1-10 [\#1232](https://github.com/kubernetes/ingress-gce/pull/1232) ([spencerhance](https://github.com/spencerhance))
- Cherry-pick \#1209  \[Expose custom health check ports in the firewall\] into release-1-10 [\#1231](https://github.com/kubernetes/ingress-gce/pull/1231) ([spencerhance](https://github.com/spencerhance))
- Cherry Pick \#1222 \[Neg crd metrics\] [\#1229](https://github.com/kubernetes/ingress-gce/pull/1229) ([swetharepakula](https://github.com/swetharepakula))
- Cherry Pick \#1218 \[Validation generation\] [\#1228](https://github.com/kubernetes/ingress-gce/pull/1228) ([swetharepakula](https://github.com/swetharepakula))
- Cherrypick \#1225\[Bugfix http forwarding rule create workflow\] into release-10 [\#1226](https://github.com/kubernetes/ingress-gce/pull/1226) ([skmatti](https://github.com/skmatti))

## [v1.9.9](https://github.com/kubernetes/ingress-gce/tree/v1.9.9) (2020-08-31)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.1...v1.9.9)

**Closed issues:**

- Source IP address is lost with nginx-ingress, istio, others, behind a GCE Ingress [\#1219](https://github.com/kubernetes/ingress-gce/issues/1219)

**Merged pull requests:**

- Cherry-pick \#1233 \[Add missing features to ingress metrics\] into release-1-9 [\#1237](https://github.com/kubernetes/ingress-gce/pull/1237) ([spencerhance](https://github.com/spencerhance))
- Cherry-pick  \#1209 \[Expose custom health check ports in the firewall\] into release-1-9 [\#1220](https://github.com/kubernetes/ingress-gce/pull/1220) ([spencerhance](https://github.com/spencerhance))
- Cherry pick \#1174 \[Implement support for Internal/Regional Static IP for L7-ILB\] into release-1.9 [\#1201](https://github.com/kubernetes/ingress-gce/pull/1201) ([spencerhance](https://github.com/spencerhance))
- Cherry pick \#1173 \[Update k8s-cloud-provider to 1.13.0\] into release 1.9 [\#1200](https://github.com/kubernetes/ingress-gce/pull/1200) ([spencerhance](https://github.com/spencerhance))

## [v1.10.1](https://github.com/kubernetes/ingress-gce/tree/v1.10.1) (2020-08-11)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.10.0...v1.10.1)

**Merged pull requests:**

- Cherry pick \#1212 \[Fix negv1beta1 import\] [\#1213](https://github.com/kubernetes/ingress-gce/pull/1213) ([swetharepakula](https://github.com/swetharepakula))

## [v1.10.0](https://github.com/kubernetes/ingress-gce/tree/v1.10.0) (2020-08-06)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.8...v1.10.0)

**Merged pull requests:**

- Add e2e tests for neg naming and neg crd [\#1207](https://github.com/kubernetes/ingress-gce/pull/1207) ([swetharepakula](https://github.com/swetharepakula))
- Implement support for HTTPS Redirects [\#1206](https://github.com/kubernetes/ingress-gce/pull/1206) ([spencerhance](https://github.com/spencerhance))
- Allow modifying protocol in L4 ILB service [\#1204](https://github.com/kubernetes/ingress-gce/pull/1204) ([prameshj](https://github.com/prameshj))
- Add NegName to syncer key [\#1202](https://github.com/kubernetes/ingress-gce/pull/1202) ([swetharepakula](https://github.com/swetharepakula))
- Ignore deletion timestamp when ensuring NEG CR [\#1199](https://github.com/kubernetes/ingress-gce/pull/1199) ([swetharepakula](https://github.com/swetharepakula))
- Refactor tls package into translator [\#1198](https://github.com/kubernetes/ingress-gce/pull/1198) ([spencerhance](https://github.com/spencerhance))
- Use NEG CRs for NEG Garbage Collection [\#1197](https://github.com/kubernetes/ingress-gce/pull/1197) ([swetharepakula](https://github.com/swetharepakula))
- Add repo SECURITY.md [\#1196](https://github.com/kubernetes/ingress-gce/pull/1196) ([joelsmith](https://github.com/joelsmith))
- Handle error when calculating endpoints for NEG. [\#1192](https://github.com/kubernetes/ingress-gce/pull/1192) ([prameshj](https://github.com/prameshj))
- Allow modifying trafficPolicy for L4 ILB services [\#1191](https://github.com/kubernetes/ingress-gce/pull/1191) ([prameshj](https://github.com/prameshj))
- Revert to individual secret get [\#1188](https://github.com/kubernetes/ingress-gce/pull/1188) ([liggitt](https://github.com/liggitt))
- syncer writes neg desc on initialization [\#1187](https://github.com/kubernetes/ingress-gce/pull/1187) ([swetharepakula](https://github.com/swetharepakula))
- Cleanup L4 ILB IP Reservation in all cases. [\#1181](https://github.com/kubernetes/ingress-gce/pull/1181) ([prameshj](https://github.com/prameshj))
- Fix VM\_IP\_NEG creation and updates [\#1180](https://github.com/kubernetes/ingress-gce/pull/1180) ([prameshj](https://github.com/prameshj))
- Cleanup Internal IP reservation after L4 ILB creation [\#1179](https://github.com/kubernetes/ingress-gce/pull/1179) ([prameshj](https://github.com/prameshj))
- Compute number of L4 ILBs in error and success state [\#1176](https://github.com/kubernetes/ingress-gce/pull/1176) ([skmatti](https://github.com/skmatti))
- Update GKE version mapping [\#1175](https://github.com/kubernetes/ingress-gce/pull/1175) ([skmatti](https://github.com/skmatti))
- Implement support for Internal/Regional Static IP for L7-ILB [\#1174](https://github.com/kubernetes/ingress-gce/pull/1174) ([spencerhance](https://github.com/spencerhance))
- Update k8s-cloud-provider to 1.13.0 [\#1173](https://github.com/kubernetes/ingress-gce/pull/1173) ([spencerhance](https://github.com/spencerhance))
- Create corresponding Neg CRs when NEG CRD is enabled [\#1172](https://github.com/kubernetes/ingress-gce/pull/1172) ([swetharepakula](https://github.com/swetharepakula))
- Initial Refactor of TargetProxy to Translator Package [\#1168](https://github.com/kubernetes/ingress-gce/pull/1168) ([spencerhance](https://github.com/spencerhance))
- Move APIs to work with health checks into translator package to prepare for future refactoring [\#1167](https://github.com/kubernetes/ingress-gce/pull/1167) ([rramkumar1](https://github.com/rramkumar1))
- Neg crd feature gate [\#1166](https://github.com/kubernetes/ingress-gce/pull/1166) ([swetharepakula](https://github.com/swetharepakula))
- Avoid unnecessary backend service update in NEG linker [\#1162](https://github.com/kubernetes/ingress-gce/pull/1162) ([skmatti](https://github.com/skmatti))
- Update generated code for frontendconfig CRD [\#1160](https://github.com/kubernetes/ingress-gce/pull/1160) ([skmatti](https://github.com/skmatti))
- Add open API validation for Security Policy  [\#1159](https://github.com/kubernetes/ingress-gce/pull/1159) ([skmatti](https://github.com/skmatti))
- Fix broken link in faq [\#1157](https://github.com/kubernetes/ingress-gce/pull/1157) ([knrt10](https://github.com/knrt10))
- Commented out broken link [\#1156](https://github.com/kubernetes/ingress-gce/pull/1156) ([Gituser143](https://github.com/Gituser143))
- add logging in NEG syncer [\#1155](https://github.com/kubernetes/ingress-gce/pull/1155) ([freehan](https://github.com/freehan))
- Add NEG CRD definition [\#1154](https://github.com/kubernetes/ingress-gce/pull/1154) ([swetharepakula](https://github.com/swetharepakula))
- Wait for NEGs deletion based on deletion option [\#1145](https://github.com/kubernetes/ingress-gce/pull/1145) ([freehan](https://github.com/freehan))
- Ensure ingress cleanup when finalizer exists [\#1144](https://github.com/kubernetes/ingress-gce/pull/1144) ([skmatti](https://github.com/skmatti))
- Set access logging sampling rate to be 1 [\#1138](https://github.com/kubernetes/ingress-gce/pull/1138) ([skmatti](https://github.com/skmatti))
- Add events for major lifecycle events [\#1137](https://github.com/kubernetes/ingress-gce/pull/1137) ([bowei](https://github.com/bowei))
- Remove cluster-service annotation to avoid conflicting with GKE addon… [\#1136](https://github.com/kubernetes/ingress-gce/pull/1136) ([bowei](https://github.com/bowei))
- run-local-glbc.sh needs to capture stderr in the log [\#1135](https://github.com/kubernetes/ingress-gce/pull/1135) ([bowei](https://github.com/bowei))
- Remove unnecessary `update` permissions [\#1134](https://github.com/kubernetes/ingress-gce/pull/1134) ([skmatti](https://github.com/skmatti))
- Update changelog for multiple release versions [\#1133](https://github.com/kubernetes/ingress-gce/pull/1133) ([skmatti](https://github.com/skmatti))
- Update GKE version mappings [\#1130](https://github.com/kubernetes/ingress-gce/pull/1130) ([skmatti](https://github.com/skmatti))
- Cherry-pick \#1128 to release-1.9 \[Add rbac permissions for networking.gke.io/frontendconfigs\] [\#1129](https://github.com/kubernetes/ingress-gce/pull/1129) ([spencerhance](https://github.com/spencerhance))
- Add rbac permissions for networking.gke.io/frontendconfigs [\#1128](https://github.com/kubernetes/ingress-gce/pull/1128) ([spencerhance](https://github.com/spencerhance))
- Add translation for ForwardingRule [\#1126](https://github.com/kubernetes/ingress-gce/pull/1126) ([rramkumar1](https://github.com/rramkumar1))
- Replace Ingress resource Update calls with Patch [\#1125](https://github.com/kubernetes/ingress-gce/pull/1125) ([skmatti](https://github.com/skmatti))
- Add back local run script which was deleted [\#1124](https://github.com/kubernetes/ingress-gce/pull/1124) ([rramkumar1](https://github.com/rramkumar1))
- Use patch for updating ingress/service finalizers [\#1123](https://github.com/kubernetes/ingress-gce/pull/1123) ([skmatti](https://github.com/skmatti))
- Force send Enable field for LogConfig [\#1119](https://github.com/kubernetes/ingress-gce/pull/1119) ([skmatti](https://github.com/skmatti))
- Update generated code for BackendConfig [\#1116](https://github.com/kubernetes/ingress-gce/pull/1116) ([skmatti](https://github.com/skmatti))
- Script to run local e2e test [\#1110](https://github.com/kubernetes/ingress-gce/pull/1110) ([bowei](https://github.com/bowei))
- Wait for caches to sync before running node sync [\#1107](https://github.com/kubernetes/ingress-gce/pull/1107) ([bowei](https://github.com/bowei))
- do not sync IG when not necessary [\#1105](https://github.com/kubernetes/ingress-gce/pull/1105) ([freehan](https://github.com/freehan))
- Change project permissions check from 'foo' to 'k8s-ingress-svc-acct-… [\#1104](https://github.com/kubernetes/ingress-gce/pull/1104) ([bowei](https://github.com/bowei))
- Add test for pruning unknown fields of CRDs [\#1103](https://github.com/kubernetes/ingress-gce/pull/1103) ([skmatti](https://github.com/skmatti))
- Wait for backendconfig CRD to be established [\#1102](https://github.com/kubernetes/ingress-gce/pull/1102) ([skmatti](https://github.com/skmatti))
- Initial refactors to bootstrap self-contained translator [\#1101](https://github.com/kubernetes/ingress-gce/pull/1101) ([rramkumar1](https://github.com/rramkumar1))
- Fix TestILBError test for NEG defaulting [\#1100](https://github.com/kubernetes/ingress-gce/pull/1100) ([freehan](https://github.com/freehan))
- Enable pruning of unknown fields for CRDs [\#1096](https://github.com/kubernetes/ingress-gce/pull/1096) ([skmatti](https://github.com/skmatti))
- Add e2e tests for backendconfig access logging [\#1094](https://github.com/kubernetes/ingress-gce/pull/1094) ([skmatti](https://github.com/skmatti))
- Add support for Port to BackendConfig HealthCheckConfig [\#1092](https://github.com/kubernetes/ingress-gce/pull/1092) ([spencerhance](https://github.com/spencerhance))
- Update L7-ILB API to GA [\#1091](https://github.com/kubernetes/ingress-gce/pull/1091) ([spencerhance](https://github.com/spencerhance))
- Replace GCE\_PRIMARY\_VM\_IP to GCE\_VM\_IP NEG. [\#1090](https://github.com/kubernetes/ingress-gce/pull/1090) ([prameshj](https://github.com/prameshj))
- Add transition e2e test to healthcheck [\#1086](https://github.com/kubernetes/ingress-gce/pull/1086) ([bowei](https://github.com/bowei))
- Add e2e test for configuring healthchecks via BackendConfig [\#1084](https://github.com/kubernetes/ingress-gce/pull/1084) ([bowei](https://github.com/bowei))
- e2e tests for SslPolicy [\#1083](https://github.com/kubernetes/ingress-gce/pull/1083) ([spencerhance](https://github.com/spencerhance))
- Update FrontendConfig v1beta1 API so that Spec and Status fields are optional [\#1082](https://github.com/kubernetes/ingress-gce/pull/1082) ([spencerhance](https://github.com/spencerhance))
- Fail ingress creation if specified staticIP name does not exist [\#1080](https://github.com/kubernetes/ingress-gce/pull/1080) ([prameshj](https://github.com/prameshj))
- Update non-GCP deployment instructions to work with GKE On-Prem cluster instead of GCP GKE [\#1079](https://github.com/kubernetes/ingress-gce/pull/1079) ([cxhiano](https://github.com/cxhiano))
- Allow local zone override for NonGCP NEG [\#1077](https://github.com/kubernetes/ingress-gce/pull/1077) ([freehan](https://github.com/freehan))
- Return GCE LB deletion error [\#1076](https://github.com/kubernetes/ingress-gce/pull/1076) ([skmatti](https://github.com/skmatti))
- Update go dependencies to k8s v1.18.0 [\#1070](https://github.com/kubernetes/ingress-gce/pull/1070) ([aledbf](https://github.com/aledbf))
- Fix json tag for Logging [\#1066](https://github.com/kubernetes/ingress-gce/pull/1066) ([skmatti](https://github.com/skmatti))
- Track count of user specified Static Global IPs [\#1065](https://github.com/kubernetes/ingress-gce/pull/1065) ([skmatti](https://github.com/skmatti))
- Increase GLBC timeout to 60 minutes [\#1063](https://github.com/kubernetes/ingress-gce/pull/1063) ([skmatti](https://github.com/skmatti))
- Update BackendConfig LoadBalancingScheme for L7-ILB to INTERNAL\_MANAGED [\#1062](https://github.com/kubernetes/ingress-gce/pull/1062) ([spencerhance](https://github.com/spencerhance))
- Increase GCLB deletion timeout [\#1061](https://github.com/kubernetes/ingress-gce/pull/1061) ([skmatti](https://github.com/skmatti))
- Handle health-check not found error gracefully [\#1058](https://github.com/kubernetes/ingress-gce/pull/1058) ([skmatti](https://github.com/skmatti))
- Fix kube-config for CRD informers [\#1051](https://github.com/kubernetes/ingress-gce/pull/1051) ([skmatti](https://github.com/skmatti))
- trivial: fixed broken link in Markdown [\#1048](https://github.com/kubernetes/ingress-gce/pull/1048) ([zioproto](https://github.com/zioproto))
- Use backendconfig v1 for e2e tests [\#1045](https://github.com/kubernetes/ingress-gce/pull/1045) ([skmatti](https://github.com/skmatti))
- Add support for Access Logs in BackendConfig [\#1041](https://github.com/kubernetes/ingress-gce/pull/1041) ([skmatti](https://github.com/skmatti))
- Fix error message emitted during firewall create/update when using XPN. [\#1016](https://github.com/kubernetes/ingress-gce/pull/1016) ([prameshj](https://github.com/prameshj))

## [v1.9.8](https://github.com/kubernetes/ingress-gce/tree/v1.9.8) (2020-07-21)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.7...v1.9.8)

**Implemented enhancements:**

- sync error regression in 1.5.1 [\#713](https://github.com/kubernetes/ingress-gce/issues/713)
- Tracking issue for FrontendConfig work [\#515](https://github.com/kubernetes/ingress-gce/issues/515)

**Fixed bugs:**

- Ingress for Internal L7 LB creates backend-service with wrong scheme [\#1059](https://github.com/kubernetes/ingress-gce/issues/1059)
- ingress creation should fail if static ip annotation does not exist [\#1005](https://github.com/kubernetes/ingress-gce/issues/1005)
- Changing app-protocol from HTTP2 to other protocols will cause glbc to hit nil pointer  [\#675](https://github.com/kubernetes/ingress-gce/issues/675)

**Closed issues:**

- NEG service loses zone when single node in zone is cordoned \(multi-zone node pool\) [\#1183](https://github.com/kubernetes/ingress-gce/issues/1183)
- Multi host ingress is using wrong certificate for single host [\#1178](https://github.com/kubernetes/ingress-gce/issues/1178)
- Update GKE Version Mapping [\#1153](https://github.com/kubernetes/ingress-gce/issues/1153)
- MultiClusterIngress with CDN [\#1093](https://github.com/kubernetes/ingress-gce/issues/1093)
- How to setup minReadySeconds/terminationGracePeriodSeconds with NEG? [\#1072](https://github.com/kubernetes/ingress-gce/issues/1072)
- Healthcheck configuration behavior [\#1009](https://github.com/kubernetes/ingress-gce/issues/1009)
- Ingress: http status code 413 Request entity too large [\#982](https://github.com/kubernetes/ingress-gce/issues/982)

**Merged pull requests:**

- Cherrypick \#1144 \[Ensure ingress cleanup when finalizer exists\] into release-1.9 [\#1194](https://github.com/kubernetes/ingress-gce/pull/1194) ([skmatti](https://github.com/skmatti))
- Handle error when calculating endpoints for NEG. [\#1193](https://github.com/kubernetes/ingress-gce/pull/1193) ([prameshj](https://github.com/prameshj))

## [v1.9.7](https://github.com/kubernetes/ingress-gce/tree/v1.9.7) (2020-06-16)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.8.3...v1.9.7)

**Closed issues:**

- Error during sync: error running backend syncing routine: unable to find nodeport in any service [\#1146](https://github.com/kubernetes/ingress-gce/issues/1146)
- LoadBalancerNegNotReady [\#931](https://github.com/kubernetes/ingress-gce/issues/931)

**Merged pull requests:**

- Cherrypick \#1159 \[Fix Security Policy Config for Open API gen\] into release-1.9 [\#1161](https://github.com/kubernetes/ingress-gce/pull/1161) ([skmatti](https://github.com/skmatti))
- Cherrypicking \#1155 into release 1.9 [\#1158](https://github.com/kubernetes/ingress-gce/pull/1158) ([freehan](https://github.com/freehan))

## [v1.8.3](https://github.com/kubernetes/ingress-gce/tree/v1.8.3) (2020-06-08)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.6.4...v1.8.3)

**Merged pull requests:**

- Cherrypick \#1138 \[Set access logging sampling rate to be 1\] into release-1.8 [\#1141](https://github.com/kubernetes/ingress-gce/pull/1141) ([skmatti](https://github.com/skmatti))
- Cherrypick \#1076\[Return GCE LB deletion error\] into release-1.8 [\#1098](https://github.com/kubernetes/ingress-gce/pull/1098) ([skmatti](https://github.com/skmatti))
- Cherrypick \#1051\[Fix kube-config for CRD informers\] into release-1.8 [\#1052](https://github.com/kubernetes/ingress-gce/pull/1052) ([skmatti](https://github.com/skmatti))

## [v1.6.4](https://github.com/kubernetes/ingress-gce/tree/v1.6.4) (2020-06-08)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.7.5...v1.6.4)

**Merged pull requests:**

- Cherrypick \#1138 \[Set access logging sampling rate to be 1\] into release-1.6 [\#1139](https://github.com/kubernetes/ingress-gce/pull/1139) ([skmatti](https://github.com/skmatti))
- Revert \#849\[Use protobufs for communication with apiserver\] in v1.6 [\#1050](https://github.com/kubernetes/ingress-gce/pull/1050) ([skmatti](https://github.com/skmatti))

## [v1.7.5](https://github.com/kubernetes/ingress-gce/tree/v1.7.5) (2020-06-08)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.6...v1.7.5)

**Merged pull requests:**

- Cherrypick \#1138 \[Set access logging sampling rate to be 1\] into release-1.7 [\#1140](https://github.com/kubernetes/ingress-gce/pull/1140) ([skmatti](https://github.com/skmatti))

## [v1.9.6](https://github.com/kubernetes/ingress-gce/tree/v1.9.6) (2020-06-08)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.5...v1.9.6)

**Merged pull requests:**

- Cherrypick \#1138 \[Set access logging sampling rate to be 1\] into release-1.9 [\#1142](https://github.com/kubernetes/ingress-gce/pull/1142) ([skmatti](https://github.com/skmatti))

# Changelog

## [v1.9.5](https://github.com/kubernetes/ingress-gce/tree/v1.9.5) (2020-06-03)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.4...v1.9.5)

**Closed issues:**

- docs: creating HTTP LB and adding GKE services as backends after the fact [\#1122](https://github.com/kubernetes/ingress-gce/issues/1122)
- Ingress Update Issue/s \(Changing Backend/Rules\) [\#1117](https://github.com/kubernetes/ingress-gce/issues/1117)

**Merged pull requests:**

- Cherry-pick \#1128 to release-1.9 \[Add rbac permissions for networking.gke.io/frontendconfigs\] [\#1129](https://github.com/kubernetes/ingress-gce/pull/1129) ([spencerhance](https://github.com/spencerhance))
- Cherrypick PR 1080 into release-1.9 [\#1112](https://github.com/kubernetes/ingress-gce/pull/1112) ([prameshj](https://github.com/prameshj))

## [v1.9.4](https://github.com/kubernetes/ingress-gce/tree/v1.9.4) (2020-05-27)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.3...v1.9.4)

**Fixed bugs:**

- Ingress stuck in "Creating ingress" state [\#1095](https://github.com/kubernetes/ingress-gce/issues/1095)
- OpenAPI v2 schema not published for BackendConfig [\#1055](https://github.com/kubernetes/ingress-gce/issues/1055)
- Orphaned Resources After Cluster Delete [\#136](https://github.com/kubernetes/ingress-gce/issues/136)

**Closed issues:**

- GCE resource leak with v1 frontend namer  [\#965](https://github.com/kubernetes/ingress-gce/issues/965)
- Backend syncer should not block syncing if one backend fails GC [\#797](https://github.com/kubernetes/ingress-gce/issues/797)
- Feature request: ssl-redirect on gce controller [\#51](https://github.com/kubernetes/ingress-gce/issues/51)

**Merged pull requests:**

- Cherrypick \#1105 into Release-1.9 [\#1121](https://github.com/kubernetes/ingress-gce/pull/1121) ([freehan](https://github.com/freehan))
- Cherrypick \#1119 \[Force send Enable field for LogConfig\] into release-1.9 [\#1120](https://github.com/kubernetes/ingress-gce/pull/1120) ([skmatti](https://github.com/skmatti))
- Update generated code for BackendConfig in release-1.9 [\#1115](https://github.com/kubernetes/ingress-gce/pull/1115) ([skmatti](https://github.com/skmatti))
- Cherry Pick \#1107 \[Wait for caches to sync before running node sync\] to release-1.9 [\#1114](https://github.com/kubernetes/ingress-gce/pull/1114) ([spencerhance](https://github.com/spencerhance))
- Cherry Pick \#1104 \[Change project permissions check from 'foo' to 'k8s-ingress-svc-acct-...\] to release-1.9… [\#1113](https://github.com/kubernetes/ingress-gce/pull/1113) ([spencerhance](https://github.com/spencerhance))
- Cherrypick \#1096\[Enable pruning of unknown fields for CRDs\] into release-1.9 [\#1111](https://github.com/kubernetes/ingress-gce/pull/1111) ([skmatti](https://github.com/skmatti))
- Cherrypick PR 1090 to release-1.9 [\#1109](https://github.com/kubernetes/ingress-gce/pull/1109) ([prameshj](https://github.com/prameshj))
- Cherry-Pick \#1092 \[Add support for Port to BackendConfig HealthCheckConfig\] to release-1.9 [\#1108](https://github.com/kubernetes/ingress-gce/pull/1108) ([spencerhance](https://github.com/spencerhance))
- Cherrypick \#1076\[Return GCE LB deletion error\] into release-1.9 [\#1099](https://github.com/kubernetes/ingress-gce/pull/1099) ([skmatti](https://github.com/skmatti))
- Cherry-pick \#1082 to release-1.9 \[Update FrontendConfig v1beta1 API so that Spec and Status fields are omitempty\] [\#1089](https://github.com/kubernetes/ingress-gce/pull/1089) ([spencerhance](https://github.com/spencerhance))

## [v1.9.3](https://github.com/kubernetes/ingress-gce/tree/v1.9.3) (2020-04-23)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.2...v1.9.3)

**Closed issues:**

- Can't create firewall when running in self-controlled mode [\#1085](https://github.com/kubernetes/ingress-gce/issues/1085)
- Cannot patch new rules onto this resource [\#1078](https://github.com/kubernetes/ingress-gce/issues/1078)
- Reverse certificate dependencies [\#1068](https://github.com/kubernetes/ingress-gce/issues/1068)
- WebSocket not works using GCE Ingress. Ingress set UNHEALTHY status always  [\#1067](https://github.com/kubernetes/ingress-gce/issues/1067)
- Invalid value for field 'resource.IPAddress' when creating service type LoadBalancer [\#1057](https://github.com/kubernetes/ingress-gce/issues/1057)
- fix inaccessible links in the CHANGELOG.md [\#1042](https://github.com/kubernetes/ingress-gce/issues/1042)

**Merged pull requests:**

- Cherry Pick \#1062 to Release 1.9 \[Update BackendConfig LoadBalancingScheme for L7-ILB to INTERNAL\_MANAGED\] [\#1081](https://github.com/kubernetes/ingress-gce/pull/1081) ([spencerhance](https://github.com/spencerhance))

## [v1.9.2](https://github.com/kubernetes/ingress-gce/tree/v1.9.2) (2020-04-07)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.1...v1.9.2)

**Closed issues:**

- GKE: IAP \> L7 \> custom host and path rules \> Error code 11 [\#765](https://github.com/kubernetes/ingress-gce/issues/765)

**Merged pull requests:**

- Cherrypick \#1065\[User specified Static Global IPs\] into release-1.9 [\#1071](https://github.com/kubernetes/ingress-gce/pull/1071) ([skmatti](https://github.com/skmatti))
- Cherrypick \#1066\[Fix json tag for Logging\] [\#1069](https://github.com/kubernetes/ingress-gce/pull/1069) ([skmatti](https://github.com/skmatti))
- Cherrypick \#1058\[Handle health check not found error gracefully\] into release-1.9 [\#1060](https://github.com/kubernetes/ingress-gce/pull/1060) ([skmatti](https://github.com/skmatti))

## [v1.9.1](https://github.com/kubernetes/ingress-gce/tree/v1.9.1) (2020-03-13)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.9.0...v1.9.1)

**Closed issues:**

- Backendconfig and Frontendconfig CRD informers break with protobuf [\#1049](https://github.com/kubernetes/ingress-gce/issues/1049)

**Merged pull requests:**

- Cherrypick \#1041\[Add support for Access Logs in BackendConfig\] into release-1.9 [\#1056](https://github.com/kubernetes/ingress-gce/pull/1056) ([skmatti](https://github.com/skmatti))
- Cherrypick \#1051\[Fix kube-config for CRD informers\] into release-1.9 [\#1054](https://github.com/kubernetes/ingress-gce/pull/1054) ([skmatti](https://github.com/skmatti))

## [v1.9.0](https://github.com/kubernetes/ingress-gce/tree/v1.9.0) (2020-03-09)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.8.0...v1.9.0)

**Implemented enhancements:**

- How do I set up RPS limit [\#670](https://github.com/kubernetes/ingress-gce/issues/670)

**Closed issues:**

- BUG \> Internal Load Balancer attempts to use regional cert [\#1047](https://github.com/kubernetes/ingress-gce/issues/1047)
- Add release tags for v1.6.2, v1.7.3 and v1.8.2 [\#1038](https://github.com/kubernetes/ingress-gce/issues/1038)
- Websocket not working with HTTP2 backend [\#855](https://github.com/kubernetes/ingress-gce/issues/855)
- Ingress not Working With Websocket [\#793](https://github.com/kubernetes/ingress-gce/issues/793)

**Merged pull requests:**

- Add feature flag `EnableBackendConfigHealthCheck` [\#1040](https://github.com/kubernetes/ingress-gce/pull/1040) ([bowei](https://github.com/bowei))
- Add Access Log field to BackendConfig API [\#1039](https://github.com/kubernetes/ingress-gce/pull/1039) ([skmatti](https://github.com/skmatti))
- Bugfix: Fix frontend resource deletion test  [\#1037](https://github.com/kubernetes/ingress-gce/pull/1037) ([skmatti](https://github.com/skmatti))
- Fix Access logs enable whitebox test [\#1036](https://github.com/kubernetes/ingress-gce/pull/1036) ([skmatti](https://github.com/skmatti))
- Split large file healthchecks.go =\> healthcheck.go,healthchecks.go [\#1032](https://github.com/kubernetes/ingress-gce/pull/1032) ([bowei](https://github.com/bowei))
- Add L4 ILB usage metrics [\#1031](https://github.com/kubernetes/ingress-gce/pull/1031) ([skmatti](https://github.com/skmatti))
- Fix backendconfig annotation key for beta [\#1030](https://github.com/kubernetes/ingress-gce/pull/1030) ([skmatti](https://github.com/skmatti))
- Set healthcheck from BackendConfig [\#1029](https://github.com/kubernetes/ingress-gce/pull/1029) ([bowei](https://github.com/bowei))
- Reduce surface area of healthchecks [\#1028](https://github.com/kubernetes/ingress-gce/pull/1028) ([bowei](https://github.com/bowei))
- Typesafe composite conversion [\#1027](https://github.com/kubernetes/ingress-gce/pull/1027) ([bowei](https://github.com/bowei))
- Move apply probe to healthchecks package [\#1026](https://github.com/kubernetes/ingress-gce/pull/1026) ([bowei](https://github.com/bowei))
- Add asm-ready as the status signal of asm mode. [\#1025](https://github.com/kubernetes/ingress-gce/pull/1025) ([cadmuxe](https://github.com/cadmuxe))
- Add whitebox test for access logs [\#1024](https://github.com/kubernetes/ingress-gce/pull/1024) ([skmatti](https://github.com/skmatti))
- Add deployment guide for GLBC in non-gcp mode. [\#1022](https://github.com/kubernetes/ingress-gce/pull/1022) ([cxhiano](https://github.com/cxhiano))
- Add SetSslPolicyForTargetHttpsProxy\(\) to composite [\#1021](https://github.com/kubernetes/ingress-gce/pull/1021) ([spencerhance](https://github.com/spencerhance))
- Add SslPolicy Field to FrontendConfig [\#1020](https://github.com/kubernetes/ingress-gce/pull/1020) ([spencerhance](https://github.com/spencerhance))
- Implement SslPolicies for TargetHttpsProxy [\#1019](https://github.com/kubernetes/ingress-gce/pull/1019) ([spencerhance](https://github.com/spencerhance))
- Update k8s-cloud-provider to 1.12.0 [\#1017](https://github.com/kubernetes/ingress-gce/pull/1017) ([spencerhance](https://github.com/spencerhance))
- Cherrypick \#976, \#984, \#990 and \#1012 \[Ingress Usage metrics\] into release 1.8 [\#1015](https://github.com/kubernetes/ingress-gce/pull/1015) ([skmatti](https://github.com/skmatti))
- Remove `DefaultBackendHealthCheckPath` as a parameter [\#1013](https://github.com/kubernetes/ingress-gce/pull/1013) ([bowei](https://github.com/bowei))
- Make metrics label Snake case compliant  [\#1012](https://github.com/kubernetes/ingress-gce/pull/1012) ([skmatti](https://github.com/skmatti))
- Remove enable-istio flag for e2e test. [\#1011](https://github.com/kubernetes/ingress-gce/pull/1011) ([cadmuxe](https://github.com/cadmuxe))
- Add HealthCheckConfig to v1beta1 BackendConfig CRD [\#1010](https://github.com/kubernetes/ingress-gce/pull/1010) ([AnishShah](https://github.com/AnishShah))
- Ignore 'resource being used by' error [\#1008](https://github.com/kubernetes/ingress-gce/pull/1008) ([skmatti](https://github.com/skmatti))
- Fix error log [\#1007](https://github.com/kubernetes/ingress-gce/pull/1007) ([skmatti](https://github.com/skmatti))
- added test case TestBasicWindows [\#1006](https://github.com/kubernetes/ingress-gce/pull/1006) ([yliaog](https://github.com/yliaog))
- Handle zones with non-uniform node count in L4 ILB Subsetting [\#1004](https://github.com/kubernetes/ingress-gce/pull/1004) ([prameshj](https://github.com/prameshj))
- Add changelog for v1.8.0 [\#998](https://github.com/kubernetes/ingress-gce/pull/998) ([spencerhance](https://github.com/spencerhance))
- Add version mapping for v1.8.0 [\#997](https://github.com/kubernetes/ingress-gce/pull/997) ([skmatti](https://github.com/skmatti))
- Adding support for healthchecks to the backend configs. [\#996](https://github.com/kubernetes/ingress-gce/pull/996) ([vbannai](https://github.com/vbannai))
- Make frontend resource deletion test verify that ingress VIP is unchanged  [\#995](https://github.com/kubernetes/ingress-gce/pull/995) ([skmatti](https://github.com/skmatti))
- Bugfix: Use Static IP to create forwarding rule when available [\#994](https://github.com/kubernetes/ingress-gce/pull/994) ([skmatti](https://github.com/skmatti))
- Graduate backendconfig to GA [\#992](https://github.com/kubernetes/ingress-gce/pull/992) ([skmatti](https://github.com/skmatti))
- Add a controller for handling L4 Internal LoadBalancer services [\#991](https://github.com/kubernetes/ingress-gce/pull/991) ([prameshj](https://github.com/prameshj))
- Register NEG usage metrics [\#990](https://github.com/kubernetes/ingress-gce/pull/990) ([skmatti](https://github.com/skmatti))
- Fix the 'Get' & 'List' Composite API to call the underlying Regional service API  [\#988](https://github.com/kubernetes/ingress-gce/pull/988) ([prameshj](https://github.com/prameshj))
- Use the Regional Service API in composite types [\#987](https://github.com/kubernetes/ingress-gce/pull/987) ([prameshj](https://github.com/prameshj))
- Fix e2e helper: WaitForNegs [\#986](https://github.com/kubernetes/ingress-gce/pull/986) ([cadmuxe](https://github.com/cadmuxe))
- Update to latest ks8-cloud-provider and compute api versions. [\#985](https://github.com/kubernetes/ingress-gce/pull/985) ([prameshj](https://github.com/prameshj))
- add neg usage metrics [\#984](https://github.com/kubernetes/ingress-gce/pull/984) ([freehan](https://github.com/freehan))
- Skip checking delete for default NEG [\#983](https://github.com/kubernetes/ingress-gce/pull/983) ([skmatti](https://github.com/skmatti))
- Fix issue with Network name [\#981](https://github.com/kubernetes/ingress-gce/pull/981) ([skmatti](https://github.com/skmatti))
- Add e2e tests for NEG asm mode. [\#980](https://github.com/kubernetes/ingress-gce/pull/980) ([cadmuxe](https://github.com/cadmuxe))
- Fix timestamp of legacy-cloud-providers dependency [\#979](https://github.com/kubernetes/ingress-gce/pull/979) ([prameshj](https://github.com/prameshj))
- Fix the TrimField max length for ASM NEG name. [\#978](https://github.com/kubernetes/ingress-gce/pull/978) ([cadmuxe](https://github.com/cadmuxe))
- fix broken link [\#977](https://github.com/kubernetes/ingress-gce/pull/977) ([ydcool](https://github.com/ydcool))
- Add Ingress usage metrics [\#976](https://github.com/kubernetes/ingress-gce/pull/976) ([skmatti](https://github.com/skmatti))
- Update legacy-cloud-provider code to pickup ILB subnet changes. [\#975](https://github.com/kubernetes/ingress-gce/pull/975) ([prameshj](https://github.com/prameshj))
- Copy the address manager code for L4 ILB from k/legacy-cloud-providers [\#974](https://github.com/kubernetes/ingress-gce/pull/974) ([prameshj](https://github.com/prameshj))
- update yaml and doc for enabling asm neg. [\#972](https://github.com/kubernetes/ingress-gce/pull/972) ([cadmuxe](https://github.com/cadmuxe))
- e2e test run.sh add network [\#971](https://github.com/kubernetes/ingress-gce/pull/971) ([spencerhance](https://github.com/spencerhance))
- Add createIlbSubnet flag to ilb e2e tests [\#970](https://github.com/kubernetes/ingress-gce/pull/970) ([spencerhance](https://github.com/spencerhance))
- RegionalGCLBForVIP\(\) Fix [\#969](https://github.com/kubernetes/ingress-gce/pull/969) ([spencerhance](https://github.com/spencerhance))
- Use kube-system UID instead of its hash to compute resource suffix [\#967](https://github.com/kubernetes/ingress-gce/pull/967) ([skmatti](https://github.com/skmatti))
- Use type LoadBalancer instead of string [\#966](https://github.com/kubernetes/ingress-gce/pull/966) ([skmatti](https://github.com/skmatti))
- Emit events only for non-nil ingresses [\#963](https://github.com/kubernetes/ingress-gce/pull/963) ([skmatti](https://github.com/skmatti))
- Fix default backend port bug for ilb neg [\#961](https://github.com/kubernetes/ingress-gce/pull/961) ([spencerhance](https://github.com/spencerhance))
- Update cloud-provider repo versions. [\#960](https://github.com/kubernetes/ingress-gce/pull/960) ([prameshj](https://github.com/prameshj))
- Support creation of GCE\_VM\_PRIMARY\_IP NEGs for L4 ILB Services. [\#959](https://github.com/kubernetes/ingress-gce/pull/959) ([prameshj](https://github.com/prameshj))
- Bugfix: Fix ingress passed to postUpgrade [\#958](https://github.com/kubernetes/ingress-gce/pull/958) ([skmatti](https://github.com/skmatti))
- Bugfix: Fix service name in upgrade test [\#957](https://github.com/kubernetes/ingress-gce/pull/957) ([skmatti](https://github.com/skmatti))
- Update finalizer tests to validate against an ingress finalizer [\#956](https://github.com/kubernetes/ingress-gce/pull/956) ([skmatti](https://github.com/skmatti))
- Add NEG mocks for alpha API and a new transactions test [\#955](https://github.com/kubernetes/ingress-gce/pull/955) ([prameshj](https://github.com/prameshj))
- Migrate Neg validator to use frontend namer factory [\#953](https://github.com/kubernetes/ingress-gce/pull/953) ([skmatti](https://github.com/skmatti))
- Migrate Finalizer upgrade test to use the upgrade framework [\#952](https://github.com/kubernetes/ingress-gce/pull/952) ([skmatti](https://github.com/skmatti))
- Migrate upgrade tests to use the upgrade framework [\#951](https://github.com/kubernetes/ingress-gce/pull/951) ([skmatti](https://github.com/skmatti))
- Error out on invalid ingress frontend configuration [\#950](https://github.com/kubernetes/ingress-gce/pull/950) ([skmatti](https://github.com/skmatti))
- Fix naming scheme on creation [\#948](https://github.com/kubernetes/ingress-gce/pull/948) ([skmatti](https://github.com/skmatti))
- fix golint failures [\#930](https://github.com/kubernetes/ingress-gce/pull/930) ([huyuwen0222](https://github.com/huyuwen0222))
- Add e2e tests for v2 frontend namer  [\#921](https://github.com/kubernetes/ingress-gce/pull/921) ([skmatti](https://github.com/skmatti))
- Add e2e tests for frontend resource leak fix [\#905](https://github.com/kubernetes/ingress-gce/pull/905) ([skmatti](https://github.com/skmatti))

## [v1.8.2](https://github.com/kubernetes/ingress-gce/tree/v1.8.2) (2020-02-24)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.8.0...v1.8.2)

**Fixed bugs:**

- Cannot Disable HTTP when using managed-certificates. HTTPS + HTTP are both provisioned [\#764](https://github.com/kubernetes/ingress-gce/issues/764)
- Container Native Load Balancing Umbrella Issue [\#583](https://github.com/kubernetes/ingress-gce/issues/583)

**Closed issues:**

- Traefik behind GCP Load Balancer, [\#1018](https://github.com/kubernetes/ingress-gce/issues/1018)
- standard\_init\_linux.go:211: exec user process caused "permission denied" [\#1000](https://github.com/kubernetes/ingress-gce/issues/1000)
- Ingress with both http and https enabled yields two separate IPs [\#993](https://github.com/kubernetes/ingress-gce/issues/993)
- Add release note for 1.8 [\#989](https://github.com/kubernetes/ingress-gce/issues/989)
- GCE Ingress multi-namespace routing [\#973](https://github.com/kubernetes/ingress-gce/issues/973)
- GCE ingress health checks [\#937](https://github.com/kubernetes/ingress-gce/issues/937)
- Consider throwing events when Readiness Reflector failed to patch pod [\#863](https://github.com/kubernetes/ingress-gce/issues/863)
- Move e2e tests from k/k into this repository [\#667](https://github.com/kubernetes/ingress-gce/issues/667)

**Merged pull requests:**

- Enable Access Logs by default [\#1035](https://github.com/kubernetes/ingress-gce/pull/1035) ([skmatti](https://github.com/skmatti))
- Cherrypick \#994\[Use Static IP to create forwarding rule when available\] into release 1.8 [\#999](https://github.com/kubernetes/ingress-gce/pull/999) ([skmatti](https://github.com/skmatti))

# Change Log

## [v1.8.0](https://github.com/kubernetes/ingress-gce/tree/v1.8.0) (2019-12-07)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.7.0...v1.8.0)

**Implemented enhancements:**

- GKE self managed setup script should have an e2e test [\#760](https://github.com/kubernetes/ingress-gce/issues/760)
- L7-ILB Support Tracking [\#731](https://github.com/kubernetes/ingress-gce/issues/731)
- BackendConfig support for user-defined request headers [\#566](https://github.com/kubernetes/ingress-gce/issues/566)
- Ingress-GCE docs overhaul [\#557](https://github.com/kubernetes/ingress-gce/issues/557)

**Fixed bugs:**

- Long namespace/ingress names cause collisions with auto-created resources on GKE [\#537](https://github.com/kubernetes/ingress-gce/issues/537)
- \[GLBC\] Changing front-end configuration does not remove unnecessary target proxies/ssl-certs [\#32](https://github.com/kubernetes/ingress-gce/issues/32)

**Closed issues:**

- How to enable authentication for GKE ingress [\#914](https://github.com/kubernetes/ingress-gce/issues/914)
- Unable to manage annotations via terraform due to chosen domain [\#908](https://github.com/kubernetes/ingress-gce/issues/908)
- Bug: Incorrect ingress key function is being used [\#879](https://github.com/kubernetes/ingress-gce/issues/879)
- V2 Frontend Naming Scheme for Ingress-GCE L7  [\#858](https://github.com/kubernetes/ingress-gce/issues/858)
- Missing Changelog for 1.6 [\#857](https://github.com/kubernetes/ingress-gce/issues/857)
- GCE Ingress creates a Network Endpoint Group with 0 configured [\#832](https://github.com/kubernetes/ingress-gce/issues/832)
- Extend the Ingress e2e framework to handle more test cases [\#782](https://github.com/kubernetes/ingress-gce/issues/782)
- health check interval adds 60sec from periodSeconds of readinessProbe unexpectedly [\#772](https://github.com/kubernetes/ingress-gce/issues/772)
- Migrate Ingress type to networking.k8s.io/v1beta1 [\#770](https://github.com/kubernetes/ingress-gce/issues/770)
- Firewall rule not updated properly with NEG if service uses name in targetPort or does not name its port [\#703](https://github.com/kubernetes/ingress-gce/issues/703)
- Add tests for multi cluster ingresses [\#71](https://github.com/kubernetes/ingress-gce/issues/71)

**Merged pull requests:**

- Cherrypick \#967\[Use kube-system UID instead of its hash to compute resource suffix\] into release 1.8 [\#968](https://github.com/kubernetes/ingress-gce/pull/968) ([skmatti](https://github.com/skmatti))
- Cherrypick \#963\[Emit events only for non-nil ingresses\] into release 1.8 [\#964](https://github.com/kubernetes/ingress-gce/pull/964) ([skmatti](https://github.com/skmatti))
- Cherry pick \#961 \[Fix default backend port bug for neg ilb\] into release-1.8 [\#962](https://github.com/kubernetes/ingress-gce/pull/962) ([spencerhance](https://github.com/spencerhance))
- Cherrypick \#950 into release 1.8 [\#954](https://github.com/kubernetes/ingress-gce/pull/954) ([skmatti](https://github.com/skmatti))
- Cherrypick \#948\[Fix naming scheme on creation\] into release 1.8 [\#949](https://github.com/kubernetes/ingress-gce/pull/949) ([skmatti](https://github.com/skmatti))
- Fix return value [\#946](https://github.com/kubernetes/ingress-gce/pull/946) ([skmatti](https://github.com/skmatti))
- Cleanup unused functions [\#945](https://github.com/kubernetes/ingress-gce/pull/945) ([skmatti](https://github.com/skmatti))
- make Generic Upgrade test run in parallel [\#944](https://github.com/kubernetes/ingress-gce/pull/944) ([freehan](https://github.com/freehan))
- change to use AppsV1 for deployment API call [\#943](https://github.com/kubernetes/ingress-gce/pull/943) ([freehan](https://github.com/freehan))
- Disabling ASM mode if the Istio CRD is not available. [\#942](https://github.com/kubernetes/ingress-gce/pull/942) ([cadmuxe](https://github.com/cadmuxe))
- Add e2e test for connection timeout with l7-ilb [\#941](https://github.com/kubernetes/ingress-gce/pull/941) ([spencerhance](https://github.com/spencerhance))
- Add session affinity e2e tests for l7-ilb [\#940](https://github.com/kubernetes/ingress-gce/pull/940) ([spencerhance](https://github.com/spencerhance))
- add upgrade test for standalone neg [\#939](https://github.com/kubernetes/ingress-gce/pull/939) ([freehan](https://github.com/freehan))
- Change return type of AggregatedListNetworkEndpointGroup in cloudprovideradapter. [\#938](https://github.com/kubernetes/ingress-gce/pull/938) ([prameshj](https://github.com/prameshj))
- iterate more than one backend for backend service status [\#936](https://github.com/kubernetes/ingress-gce/pull/936) ([freehan](https://github.com/freehan))
- add configmap based config [\#935](https://github.com/kubernetes/ingress-gce/pull/935) ([cadmuxe](https://github.com/cadmuxe))
- Use Alpha API for hybrid NEGs. [\#934](https://github.com/kubernetes/ingress-gce/pull/934) ([cxhiano](https://github.com/cxhiano))
- Fix a bug in NEG controller namedport handling [\#932](https://github.com/kubernetes/ingress-gce/pull/932) ([freehan](https://github.com/freehan))
- enable readiness reflector for Standalone NEG [\#927](https://github.com/kubernetes/ingress-gce/pull/927) ([freehan](https://github.com/freehan))
- Modify NEG libraries to use composite types. [\#926](https://github.com/kubernetes/ingress-gce/pull/926) ([prameshj](https://github.com/prameshj))
- remove transaction syncer reconciliation [\#925](https://github.com/kubernetes/ingress-gce/pull/925) ([freehan](https://github.com/freehan))
- Update forwarding rule logic for l7-ilb [\#924](https://github.com/kubernetes/ingress-gce/pull/924) ([spencerhance](https://github.com/spencerhance))
- Support more methods in composite NEGs. [\#923](https://github.com/kubernetes/ingress-gce/pull/923) ([prameshj](https://github.com/prameshj))
- move neg type into syncerKey [\#922](https://github.com/kubernetes/ingress-gce/pull/922) ([freehan](https://github.com/freehan))
- Add a flag for Non-GCP mode NEG controller. [\#920](https://github.com/kubernetes/ingress-gce/pull/920) ([cxhiano](https://github.com/cxhiano))
- NEG Namedport Fix [\#917](https://github.com/kubernetes/ingress-gce/pull/917) ([freehan](https://github.com/freehan))
- remove batch syncer and move the unit test utils [\#916](https://github.com/kubernetes/ingress-gce/pull/916) ([freehan](https://github.com/freehan))
- Bump TestAffinity transition timeout from 5 to 10 minutes [\#915](https://github.com/kubernetes/ingress-gce/pull/915) ([spencerhance](https://github.com/spencerhance))
- fix golint failures [\#913](https://github.com/kubernetes/ingress-gce/pull/913) ([FayerZhang](https://github.com/FayerZhang))
- Handle cache.DeletedFinalStateUnknown [\#912](https://github.com/kubernetes/ingress-gce/pull/912) ([spencerhance](https://github.com/spencerhance))
- Plumb region to neg validator for e2e testing [\#911](https://github.com/kubernetes/ingress-gce/pull/911) ([spencerhance](https://github.com/spencerhance))
- Fix ILB forwarding rule bug [\#906](https://github.com/kubernetes/ingress-gce/pull/906) ([spencerhance](https://github.com/spencerhance))
- Add go-cmp to dependencies  [\#904](https://github.com/kubernetes/ingress-gce/pull/904) ([skmatti](https://github.com/skmatti))
- Cleanup e2e tests to use whitebox testing [\#901](https://github.com/kubernetes/ingress-gce/pull/901) ([skmatti](https://github.com/skmatti))
- enqueue default backend for NEG processing [\#900](https://github.com/kubernetes/ingress-gce/pull/900) ([freehan](https://github.com/freehan))
- Shorten ILB e2e ingress names to less than 20 characters to avoid leaks [\#896](https://github.com/kubernetes/ingress-gce/pull/896) ([spencerhance](https://github.com/spencerhance))
- Fix ILB subnet discovery to check for VPC as well [\#895](https://github.com/kubernetes/ingress-gce/pull/895) ([spencerhance](https://github.com/spencerhance))
- Bugfix: Delete unused GCE load-balancer resources on ingress spec change [\#894](https://github.com/kubernetes/ingress-gce/pull/894) ([skmatti](https://github.com/skmatti))
- Add -network flag to e2e test framework for ILB subnet creation [\#893](https://github.com/kubernetes/ingress-gce/pull/893) ([spencerhance](https://github.com/spencerhance))
- Add V2 frontend namer [\#892](https://github.com/kubernetes/ingress-gce/pull/892) ([skmatti](https://github.com/skmatti))
- remove add on manager labels [\#889](https://github.com/kubernetes/ingress-gce/pull/889) ([freehan](https://github.com/freehan))
- Update version mapping in README [\#888](https://github.com/kubernetes/ingress-gce/pull/888) ([rramkumar1](https://github.com/rramkumar1))
- Add v1.5.2, v1.6.0 and v1.7.0 to CHANGELOG.md [\#887](https://github.com/kubernetes/ingress-gce/pull/887) ([spencerhance](https://github.com/spencerhance))
- remove "addonmanager.kubernetes.io/mode: Reconcile" annotation from [\#886](https://github.com/kubernetes/ingress-gce/pull/886) ([cadmuxe](https://github.com/cadmuxe))
- Fix CreateILBSubnet\(\) logic in e2e tests [\#885](https://github.com/kubernetes/ingress-gce/pull/885) ([spencerhance](https://github.com/spencerhance))
- Fix Ingress names for ILB e2e update test [\#884](https://github.com/kubernetes/ingress-gce/pull/884) ([spencerhance](https://github.com/spencerhance))
- Refactor ingress key function and finalizer into separate package [\#883](https://github.com/kubernetes/ingress-gce/pull/883) ([skmatti](https://github.com/skmatti))
- deploy csm neg script and yaml [\#882](https://github.com/kubernetes/ingress-gce/pull/882) ([cadmuxe](https://github.com/cadmuxe))
- BugFix: Update ingress key function used for GC [\#881](https://github.com/kubernetes/ingress-gce/pull/881) ([skmatti](https://github.com/skmatti))
- Fix basic ilb test service name [\#880](https://github.com/kubernetes/ingress-gce/pull/880) ([spencerhance](https://github.com/spencerhance))
- Fix backend services whitebox test to take into account the default backend [\#878](https://github.com/kubernetes/ingress-gce/pull/878) ([rramkumar1](https://github.com/rramkumar1))
- Check for invalid L7-ILB HTTPS configuration [\#877](https://github.com/kubernetes/ingress-gce/pull/877) ([spencerhance](https://github.com/spencerhance))
- Disable Http on ILB Https e2e tests [\#876](https://github.com/kubernetes/ingress-gce/pull/876) ([spencerhance](https://github.com/spencerhance))
- Update IngressPollTimeout to 45 minutes [\#874](https://github.com/kubernetes/ingress-gce/pull/874) ([spencerhance](https://github.com/spencerhance))
- Update CreateILBSubnet\(\) to catch 400s as well [\#873](https://github.com/kubernetes/ingress-gce/pull/873) ([spencerhance](https://github.com/spencerhance))
- Update ILB e2e test resource names [\#871](https://github.com/kubernetes/ingress-gce/pull/871) ([spencerhance](https://github.com/spencerhance))
- Add CreateILBSubnet\(\) call to all ilb e2e tests [\#870](https://github.com/kubernetes/ingress-gce/pull/870) ([spencerhance](https://github.com/spencerhance))
- Update list subnets call to Beta for L7-ILB [\#869](https://github.com/kubernetes/ingress-gce/pull/869) ([spencerhance](https://github.com/spencerhance))
- Separate out health check errors for Backends [\#867](https://github.com/kubernetes/ingress-gce/pull/867) ([spencerhance](https://github.com/spencerhance))
- Bugfix: Pass updated ingress for validation [\#865](https://github.com/kubernetes/ingress-gce/pull/865) ([skmatti](https://github.com/skmatti))
- split the error message in backend syncer health processing [\#864](https://github.com/kubernetes/ingress-gce/pull/864) ([freehan](https://github.com/freehan))
- Some documentation fixes to reflect current state of world [\#862](https://github.com/kubernetes/ingress-gce/pull/862) ([rramkumar1](https://github.com/rramkumar1))
- Cleanup backend namer workflow [\#861](https://github.com/kubernetes/ingress-gce/pull/861) ([skmatti](https://github.com/skmatti))
- Migrating existing front-end namer logic to Legacy [\#860](https://github.com/kubernetes/ingress-gce/pull/860) ([skmatti](https://github.com/skmatti))
- Refactor namer into a separate package [\#859](https://github.com/kubernetes/ingress-gce/pull/859) ([skmatti](https://github.com/skmatti))
- refactor neg controller: processService, add unittest [\#856](https://github.com/kubernetes/ingress-gce/pull/856) ([cadmuxe](https://github.com/cadmuxe))
- Update e2e framework for ILB - Part 2 [\#852](https://github.com/kubernetes/ingress-gce/pull/852) ([spencerhance](https://github.com/spencerhance))
- De-dupe additional source ranges for firewall [\#851](https://github.com/kubernetes/ingress-gce/pull/851) ([spencerhance](https://github.com/spencerhance))
- Add more e2e tests for L7-ILB [\#848](https://github.com/kubernetes/ingress-gce/pull/848) ([spencerhance](https://github.com/spencerhance))
- Use protobufs for communication with apiserver [\#847](https://github.com/kubernetes/ingress-gce/pull/847) ([wojtek-t](https://github.com/wojtek-t))
- e2e test framework add support for ilb subnet [\#846](https://github.com/kubernetes/ingress-gce/pull/846) ([spencerhance](https://github.com/spencerhance))
- Remove word 'Basic' from ILB e2e test [\#845](https://github.com/kubernetes/ingress-gce/pull/845) ([rramkumar1](https://github.com/rramkumar1))
- Add supplementary e2e tests for Finalizer [\#844](https://github.com/kubernetes/ingress-gce/pull/844) ([skmatti](https://github.com/skmatti))
- Add support for composite types for zonal resources [\#804](https://github.com/kubernetes/ingress-gce/pull/804) ([prameshj](https://github.com/prameshj))
- Add scaffolding for supporting additional whitebox testing [\#603](https://github.com/kubernetes/ingress-gce/pull/603) ([rramkumar1](https://github.com/rramkumar1))

## [v1.7.4](https://github.com/kubernetes/ingress-gce/tree/v1.7.4) (2020-05-11)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.7.3...v1.7.4)

**Merged pull requests:**

- Cherrypick \#1076\[Return GCE LB deletion error\] into release-1.7 [\#1097](https://github.com/kubernetes/ingress-gce/pull/1097) ([skmatti](https://github.com/skmatti))
- Cherrypick \#1051\[Fix kube-config for CRD informers\] into release-1.7 [\#1053](https://github.com/kubernetes/ingress-gce/pull/1053) ([skmatti](https://github.com/skmatti))

## [v1.7.3](https://github.com/kubernetes/ingress-gce/tree/v1.7.3) (2020-02-24)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.7.0...v1.7.3)

**Merged pull requests:**

- Update dependency google.golang.org/api to c3b745b [\#1033](https://github.com/kubernetes/ingress-gce/pull/1033) ([skmatti](https://github.com/skmatti))
- Enable Access Logs by default [\#1023](https://github.com/kubernetes/ingress-gce/pull/1023) ([skmatti](https://github.com/skmatti))
- Cherrypick \#946 into Release 1.7 [\#947](https://github.com/kubernetes/ingress-gce/pull/947) ([skmatti](https://github.com/skmatti))
- Cherry pick \#912 \[Handle cache.DeletedFinalStateUnknown\] to release-1.7 [\#918](https://github.com/kubernetes/ingress-gce/pull/918) ([spencerhance](https://github.com/spencerhance))
- Cherry Pick \#906 \(Fix ILB forwarding rule bug\) to release-1.7 [\#907](https://github.com/kubernetes/ingress-gce/pull/907) ([spencerhance](https://github.com/spencerhance))
- Cherry pick \#900 into release-1.7 [\#903](https://github.com/kubernetes/ingress-gce/pull/903) ([spencerhance](https://github.com/spencerhance))
- Cherry pick \#881 into 1.7 [\#902](https://github.com/kubernetes/ingress-gce/pull/902) ([skmatti](https://github.com/skmatti))
- Cherry pick \#895 into 1 7 [\#897](https://github.com/kubernetes/ingress-gce/pull/897) ([spencerhance](https://github.com/spencerhance))
- Cherry Pick \#869 \[Update list subnets call to Beta for L7-ILB\] to release-1.7 [\#890](https://github.com/kubernetes/ingress-gce/pull/890) ([spencerhance](https://github.com/spencerhance))

## [v1.7.0](https://github.com/kubernetes/ingress-gce/tree/v1.7.0) (2019-09-26)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.6.0...v1.7.0)

**Fixed bugs:**

- NEG controller should create NEG for default backend when enabled [\#767](https://github.com/kubernetes/ingress-gce/issues/767)
- Removing Node Pool from Cluster Breaks Ingress Controller [\#649](https://github.com/kubernetes/ingress-gce/issues/649)
- Ingress controller should react to node scale down event from autoscaler [\#595](https://github.com/kubernetes/ingress-gce/issues/595)
- BackendConfig OpenAPI spec [\#563](https://github.com/kubernetes/ingress-gce/issues/563)

**Closed issues:**

- GKE is not creating Load Balancer automatically [\#868](https://github.com/kubernetes/ingress-gce/issues/868)
- Websockets not working over TLS \(wss\) [\#854](https://github.com/kubernetes/ingress-gce/issues/854)
- Fix or verify the GC logic for multi-cluster ingress [\#836](https://github.com/kubernetes/ingress-gce/issues/836)
- Scale test is failing  [\#831](https://github.com/kubernetes/ingress-gce/issues/831)
- Duplicate rules created in load balancer [\#821](https://github.com/kubernetes/ingress-gce/issues/821)
- Unable to use TLS 1.3 [\#817](https://github.com/kubernetes/ingress-gce/issues/817)
- Is there any way an Ingress can send X-Forwarded-\* headers to the service/deployment [\#805](https://github.com/kubernetes/ingress-gce/issues/805)
- Recently , I have same issue on both clusters.... [\#801](https://github.com/kubernetes/ingress-gce/issues/801)
- Ingress not redirect to https [\#800](https://github.com/kubernetes/ingress-gce/issues/800)
- Proposal: Declarative Configuration of Multi-Cluster Ingress [\#794](https://github.com/kubernetes/ingress-gce/issues/794)
- Links to examples folder are broken in docs [\#791](https://github.com/kubernetes/ingress-gce/issues/791)
- Backend Config client import Must\(\) method from runtime package that no longer exist [\#777](https://github.com/kubernetes/ingress-gce/issues/777)
- Source IP only contains ISP address & ephemeral address [\#775](https://github.com/kubernetes/ingress-gce/issues/775)
- failureThreshold of readinessProbe is not reflected to health check [\#771](https://github.com/kubernetes/ingress-gce/issues/771)
- jobs failing with "unknown flag: --verbose" [\#700](https://github.com/kubernetes/ingress-gce/issues/700)
- Does it support a second jump? [\#657](https://github.com/kubernetes/ingress-gce/issues/657)
- Controller fail on syncing config when a service is not found [\#646](https://github.com/kubernetes/ingress-gce/issues/646)
- Defaults per cluster or project? [\#632](https://github.com/kubernetes/ingress-gce/issues/632)

**Merged pull requests:**

- Update list subnets call to Beta for L7-ILB [\#869](https://github.com/kubernetes/ingress-gce/pull/869) ([spencerhance](https://github.com/spencerhance))
- Bugfix: Pass updated ingress for validation [\#865](https://github.com/kubernetes/ingress-gce/pull/865) ([skmatti](https://github.com/skmatti))
- split the error message in backend syncer health processing [\#864](https://github.com/kubernetes/ingress-gce/pull/864) ([freehan](https://github.com/freehan))
- Some documentation fixes to reflect current state of world [\#862](https://github.com/kubernetes/ingress-gce/pull/862) ([rramkumar1](https://github.com/rramkumar1))
- Refactor namer into a separate package [\#859](https://github.com/kubernetes/ingress-gce/pull/859) ([skmatti](https://github.com/skmatti))
- De-dupe additional source ranges for firewall [\#851](https://github.com/kubernetes/ingress-gce/pull/851) ([spencerhance](https://github.com/spencerhance))
- Add more e2e tests for L7-ILB [\#848](https://github.com/kubernetes/ingress-gce/pull/848) ([spencerhance](https://github.com/spencerhance))
- Use protobufs for communication with apiserver [\#847](https://github.com/kubernetes/ingress-gce/pull/847) ([wojtek-t](https://github.com/wojtek-t))
- e2e test framework add support for ilb subnet [\#846](https://github.com/kubernetes/ingress-gce/pull/846) ([spencerhance](https://github.com/spencerhance))
- Remove word 'Basic' from ILB e2e test [\#845](https://github.com/kubernetes/ingress-gce/pull/845) ([rramkumar1](https://github.com/rramkumar1))
- Add supplementary e2e tests for Finalizer [\#844](https://github.com/kubernetes/ingress-gce/pull/844) ([skmatti](https://github.com/skmatti))
- Update e2e framework for ILB - Part 1 [\#843](https://github.com/kubernetes/ingress-gce/pull/843) ([spencerhance](https://github.com/spencerhance))
- fix a typo [\#842](https://github.com/kubernetes/ingress-gce/pull/842) ([freehan](https://github.com/freehan))
- try to spread the pods across zone to avoid test timeout [\#841](https://github.com/kubernetes/ingress-gce/pull/841) ([freehan](https://github.com/freehan))
- Bugfix: don't GC instance groups while multi-cluster ingresses exist [\#838](https://github.com/kubernetes/ingress-gce/pull/838) ([skmatti](https://github.com/skmatti))
- Switch L7-ILB versions to Beta [\#835](https://github.com/kubernetes/ingress-gce/pull/835) ([spencerhance](https://github.com/spencerhance))
- add timeout for polling NEG health status for pod readiness [\#834](https://github.com/kubernetes/ingress-gce/pull/834) ([freehan](https://github.com/freehan))
- Add e2e tests for Finalizer [\#833](https://github.com/kubernetes/ingress-gce/pull/833) ([skmatti](https://github.com/skmatti))
- add more neg tests [\#829](https://github.com/kubernetes/ingress-gce/pull/829) ([freehan](https://github.com/freehan))
- fix possible nil pointer [\#828](https://github.com/kubernetes/ingress-gce/pull/828) ([freehan](https://github.com/freehan))
- Add CSM NEG support. [\#827](https://github.com/kubernetes/ingress-gce/pull/827) ([cadmuxe](https://github.com/cadmuxe))
- add namespace in the ingress builder [\#826](https://github.com/kubernetes/ingress-gce/pull/826) ([freehan](https://github.com/freehan))
- Cleanup finalizer workflow and improve Ingress GC [\#825](https://github.com/kubernetes/ingress-gce/pull/825) ([skmatti](https://github.com/skmatti))
- follow up for \#820 [\#823](https://github.com/kubernetes/ingress-gce/pull/823) ([freehan](https://github.com/freehan))
- Legacy api e2e [\#822](https://github.com/kubernetes/ingress-gce/pull/822) ([bowei](https://github.com/bowei))
- tolerate non zonal NEG included in the AggregatedList API response [\#820](https://github.com/kubernetes/ingress-gce/pull/820) ([freehan](https://github.com/freehan))
- Refactor LB GC [\#819](https://github.com/kubernetes/ingress-gce/pull/819) ([spencerhance](https://github.com/spencerhance))
- NEG support default backend for L7-ILB [\#818](https://github.com/kubernetes/ingress-gce/pull/818) ([spencerhance](https://github.com/spencerhance))
- Get gke-self-managed.sh working on macOS [\#816](https://github.com/kubernetes/ingress-gce/pull/816) ([yfuruyama](https://github.com/yfuruyama))
- Update k8s-cloud-provider to 1.9.0 [\#815](https://github.com/kubernetes/ingress-gce/pull/815) ([spencerhance](https://github.com/spencerhance))
- Update gce.md with links that don't return 404 [\#813](https://github.com/kubernetes/ingress-gce/pull/813) ([rramkumar1](https://github.com/rramkumar1))
- Refactor loadbalancer features [\#812](https://github.com/kubernetes/ingress-gce/pull/812) ([spencerhance](https://github.com/spencerhance))
- Add cloud pointer to neg linker and backend syncer [\#811](https://github.com/kubernetes/ingress-gce/pull/811) ([spencerhance](https://github.com/spencerhance))
- Refactor Backend GC [\#810](https://github.com/kubernetes/ingress-gce/pull/810) ([spencerhance](https://github.com/spencerhance))
- make SandboxEventDump a deferred function to avoid being skipped by t.Fatal [\#809](https://github.com/kubernetes/ingress-gce/pull/809) ([freehan](https://github.com/freehan))
- fix panic caused by the ingress api migration [\#808](https://github.com/kubernetes/ingress-gce/pull/808) ([freehan](https://github.com/freehan))
- Change from "extensions.v1beta1" to "networking.v1beta1" [\#806](https://github.com/kubernetes/ingress-gce/pull/806) ([bowei](https://github.com/bowei))
- Move to k8s 1.15 [\#803](https://github.com/kubernetes/ingress-gce/pull/803) ([prameshj](https://github.com/prameshj))
- dump events in sandbox [\#802](https://github.com/kubernetes/ingress-gce/pull/802) ([freehan](https://github.com/freehan))
- Update cluster-setup.md with the right instructions for deleting the ingress-gce pod [\#799](https://github.com/kubernetes/ingress-gce/pull/799) ([rramkumar1](https://github.com/rramkumar1))
- Add beta regional resources to composite [\#798](https://github.com/kubernetes/ingress-gce/pull/798) ([spencerhance](https://github.com/spencerhance))
- HealthChecks switch from fakes to fakeGCE [\#796](https://github.com/kubernetes/ingress-gce/pull/796) ([spencerhance](https://github.com/spencerhance))
- Implement custom request headers for backend config [\#795](https://github.com/kubernetes/ingress-gce/pull/795) ([mcfedr](https://github.com/mcfedr))
- Added NoSchedule effect to GetNodeConditionPredicate [\#792](https://github.com/kubernetes/ingress-gce/pull/792) ([vinicyusmacedo](https://github.com/vinicyusmacedo))
- More composite updates for integration [\#790](https://github.com/kubernetes/ingress-gce/pull/790) ([spencerhance](https://github.com/spencerhance))
- Add support for L7-ILB \(take 2\) [\#789](https://github.com/kubernetes/ingress-gce/pull/789) ([spencerhance](https://github.com/spencerhance))
- Integrate composite types into controller [\#788](https://github.com/kubernetes/ingress-gce/pull/788) ([spencerhance](https://github.com/spencerhance))
- Rename composite files [\#787](https://github.com/kubernetes/ingress-gce/pull/787) ([spencerhance](https://github.com/spencerhance))
- Follow up on \#774 [\#786](https://github.com/kubernetes/ingress-gce/pull/786) ([freehan](https://github.com/freehan))
- Composite updates for integration [\#785](https://github.com/kubernetes/ingress-gce/pull/785) ([spencerhance](https://github.com/spencerhance))
- Redo "Remove Loadbalancer interface and use k8s-cloud-provider mocks" [\#784](https://github.com/kubernetes/ingress-gce/pull/784) ([spencerhance](https://github.com/spencerhance))
- Revert "Remove Loadbalancer interface and use k8s-cloud-provider mocks" [\#783](https://github.com/kubernetes/ingress-gce/pull/783) ([rramkumar1](https://github.com/rramkumar1))
- Remove Loadbalancer interface and use k8s-cloud-provider mocks [\#781](https://github.com/kubernetes/ingress-gce/pull/781) ([spencerhance](https://github.com/spencerhance))
- bump draining poll timeout to reduce flakes [\#779](https://github.com/kubernetes/ingress-gce/pull/779) ([krzyzacy](https://github.com/krzyzacy))
- use GlobalForwardingRules\(\).Get\(\) when detected a global fw rule [\#778](https://github.com/kubernetes/ingress-gce/pull/778) ([krzyzacy](https://github.com/krzyzacy))
- add explicit logs when fail to get a cloud resource [\#776](https://github.com/kubernetes/ingress-gce/pull/776) ([krzyzacy](https://github.com/krzyzacy))

## [v1.6.2](https://github.com/kubernetes/ingress-gce/tree/v1.6.2) (2020-02-28)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.6.1...v1.6.2)

**Merged pull requests:**

- Enable Logging for BackendService by default [\#1044](https://github.com/kubernetes/ingress-gce/pull/1044) ([skmatti](https://github.com/skmatti))

## [v1.6.0](https://github.com/kubernetes/ingress-gce/tree/v1.6.0) (2019-06-14)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.5.2...v1.6.0)

**Implemented enhancements:**

- Unclear documentation of spec.rules.http.paths [\#181](https://github.com/kubernetes/ingress-gce/issues/181)
- Add example using grpc and http2  [\#18](https://github.com/kubernetes/ingress-gce/issues/18)

**Closed issues:**

- HTTPS frontend listener isn't deleted after setting ingress.allow-http: "false" annotation [\#766](https://github.com/kubernetes/ingress-gce/issues/766)
- Backends healthchecks and expected operation [\#762](https://github.com/kubernetes/ingress-gce/issues/762)
- Update GKE self managed script [\#758](https://github.com/kubernetes/ingress-gce/issues/758)
- Deploying to GKE self managed has invalid YAML [\#755](https://github.com/kubernetes/ingress-gce/issues/755)
- problems with spdy/http2 for some urls - net::ERR\_SPDY\_PROTOCOL\_ERROR net::ERR\_INCOMPLETE\_CHUNKED\_ENCODING [\#749](https://github.com/kubernetes/ingress-gce/issues/749)
- Controller not syncing LoadBalancer IP when certificate is invalid [\#733](https://github.com/kubernetes/ingress-gce/issues/733)
- Measure and Adjust Resource Request Limit  [\#55](https://github.com/kubernetes/ingress-gce/issues/55)

**Merged pull requests:**

- Follow up on Readiness Reflector [\#774](https://github.com/kubernetes/ingress-gce/pull/774) ([freehan](https://github.com/freehan))
- Revendor K8s Cloud Provider [\#768](https://github.com/kubernetes/ingress-gce/pull/768) ([freehan](https://github.com/freehan))
- Parse the gce endpoint flag the same way as k8s [\#763](https://github.com/kubernetes/ingress-gce/pull/763) ([krzyzacy](https://github.com/krzyzacy))
- Add option to override gce endpoint in test [\#761](https://github.com/kubernetes/ingress-gce/pull/761) ([krzyzacy](https://github.com/krzyzacy))
- Updates for usability of GKE self managed setup script [\#759](https://github.com/kubernetes/ingress-gce/pull/759) ([KatrinaHoffert](https://github.com/KatrinaHoffert))
- GCP forwarding rule/target proxy description field populated [\#757](https://github.com/kubernetes/ingress-gce/pull/757) ([KatrinaHoffert](https://github.com/KatrinaHoffert))
- Deploying to GKE self managed has invalid YAML [\#756](https://github.com/kubernetes/ingress-gce/pull/756) ([KatrinaHoffert](https://github.com/KatrinaHoffert))
- flag gate readiness reflector [\#754](https://github.com/kubernetes/ingress-gce/pull/754) ([freehan](https://github.com/freehan))
- Rebase of \#559 "Scaffolding for FrontendConfig" [\#753](https://github.com/kubernetes/ingress-gce/pull/753) ([spencerhance](https://github.com/spencerhance))
- Emit event if Ingress spec does not contain valid config to setup frontend resources [\#752](https://github.com/kubernetes/ingress-gce/pull/752) ([rramkumar1](https://github.com/rramkumar1))
- readiness reflector [\#748](https://github.com/kubernetes/ingress-gce/pull/748) ([freehan](https://github.com/freehan))
- Adding the /healthz handler to the 404-default-server-with-metrics to … [\#747](https://github.com/kubernetes/ingress-gce/pull/747) ([vbannai](https://github.com/vbannai))
- Update canonical rbac.yaml with latest, minimal bindings [\#746](https://github.com/kubernetes/ingress-gce/pull/746) ([dekkagaijin](https://github.com/dekkagaijin))
- Adding docker configuration file for the 404-server-with-metrics and … [\#745](https://github.com/kubernetes/ingress-gce/pull/745) ([vbannai](https://github.com/vbannai))
- More composite types [\#742](https://github.com/kubernetes/ingress-gce/pull/742) ([spencerhance](https://github.com/spencerhance))
- Switch to go modules [\#735](https://github.com/kubernetes/ingress-gce/pull/735) ([spencerhance](https://github.com/spencerhance))

## [v1.5.2](https://github.com/kubernetes/ingress-gce/tree/v1.5.2) (2019-05-01)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.5.1...v1.5.2)

**Implemented enhancements:**

- Link for the example on deploying ingress controller is not valid [\#686](https://github.com/kubernetes/ingress-gce/issues/686)
- If readiness probe is on port different than the service \(app\) port - ingress fails to sync the correct healthcheck [\#647](https://github.com/kubernetes/ingress-gce/issues/647)

**Fixed bugs:**

- The networking.gke.io/suppress-firewall-xpn-error: true annotation doesn't in combination with kubernetes.io/ingress.global-static-ip-name [\#569](https://github.com/kubernetes/ingress-gce/issues/569)

**Closed issues:**

- Cannot Disable HTTP when using managed-certificates. Results in infinite Creating ingress and 'no frontend configuration'  [\#738](https://github.com/kubernetes/ingress-gce/issues/738)
- not picking up TLS secret updates [\#724](https://github.com/kubernetes/ingress-gce/issues/724)
- HealthCheck cannot get ReadinessProbe [\#717](https://github.com/kubernetes/ingress-gce/issues/717)
- Configuring SSL Policies [\#716](https://github.com/kubernetes/ingress-gce/issues/716)
- Add option to enable proxy protocol [\#699](https://github.com/kubernetes/ingress-gce/issues/699)
- Disabling/Configuring HSTS [\#693](https://github.com/kubernetes/ingress-gce/issues/693)
- Confusion about the root health check [\#674](https://github.com/kubernetes/ingress-gce/issues/674)
- ERR\_SPDY\_PROTOCOL\_ERROR when streaming text/event-stream  [\#518](https://github.com/kubernetes/ingress-gce/issues/518)
- unable to activate gzip [\#493](https://github.com/kubernetes/ingress-gce/issues/493)
- Deprecate echoserver and add new container image based on cmd/echo  [\#427](https://github.com/kubernetes/ingress-gce/issues/427)

**Merged pull requests:**

- Cherrypick \#728 onto 1.5 branch.  [\#741](https://github.com/kubernetes/ingress-gce/pull/741) ([rramkumar1](https://github.com/rramkumar1))

# Change Log

## [v1.5.1](https://github.com/kubernetes/ingress-gce/tree/v1.5.1) (2019-03-15)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.5.0...v1.5.1)

**Fixed bugs:**

- \[GLBC\] GCE resources of non-snapshotted ingresses are not deleted [\#31](https://github.com/kubernetes/ingress-gce/issues/31)

**Closed issues:**

- Why GCP deploy an ingress controller on the master node rather than worker node? [\#685](https://github.com/kubernetes/ingress-gce/issues/685)
- How to expose ports [\#634](https://github.com/kubernetes/ingress-gce/issues/634)
- load balancer controller out of sync with gcp and ingress annotations [\#562](https://github.com/kubernetes/ingress-gce/issues/562)
- Example service and ingress gives Unknown Host error [\#560](https://github.com/kubernetes/ingress-gce/issues/560)
- GKE Ingress controller ignoring ingress.class annotation [\#476](https://github.com/kubernetes/ingress-gce/issues/476)

**Merged pull requests:**

- Cherry pick \#688 into release.15 [\#692](https://github.com/kubernetes/ingress-gce/pull/692) ([freehan](https://github.com/freehan))
- Cherrypick \#678 into release-1.5 [\#691](https://github.com/kubernetes/ingress-gce/pull/691) ([freehan](https://github.com/freehan))
- Fix supporting secret-based and pre-shared certs at the same time [\#687](https://github.com/kubernetes/ingress-gce/pull/687) ([michallowicki](https://github.com/michallowicki))

# Change Log

## [v1.5.0](https://github.com/kubernetes/ingress-gce/tree/v1.5.0) (2019-02-27)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.4.0...v1.5.0)

**Implemented enhancements:**

- Update version mapping for GKE 1.11.2 [\#535](https://github.com/kubernetes/ingress-gce/issues/535)
- Is github release tab forgotten or abandoned? [\#534](https://github.com/kubernetes/ingress-gce/issues/534)
- Certificate update path is not clear [\#525](https://github.com/kubernetes/ingress-gce/issues/525)
- Support session affinity via Ingress annotation [\#516](https://github.com/kubernetes/ingress-gce/issues/516)

**Fixed bugs:**

- BackendConfig `cdn: enable: true` uses scary defaults [\#599](https://github.com/kubernetes/ingress-gce/issues/599)
- Instance is Not Removed from IG when node is marked as unschedulable [\#591](https://github.com/kubernetes/ingress-gce/issues/591)
- Readiness Probe does not get reflected for NEG enabled ingress [\#541](https://github.com/kubernetes/ingress-gce/issues/541)
- Firewall rule required message ignores existing rules \(shared VPC\) [\#485](https://github.com/kubernetes/ingress-gce/issues/485)
- Ingress-GCE does not GC LB resources when ingress class changed and no other ingress on GCE exists [\#481](https://github.com/kubernetes/ingress-gce/issues/481)
- ingress controller gave me 2 IP addresses instead of 1 when I added TLS [\#410](https://github.com/kubernetes/ingress-gce/issues/410)

**Closed issues:**

- GCP - Kubernetes Ingress Backend service unhealthy [\#621](https://github.com/kubernetes/ingress-gce/issues/621)
- BackendConfig security policy not enforced  [\#616](https://github.com/kubernetes/ingress-gce/issues/616)
- Multiple e2e jobs are failing [\#606](https://github.com/kubernetes/ingress-gce/issues/606)
- GKE ingress stuck in creating after deploying ingress 1.11.5.gke.5 [\#605](https://github.com/kubernetes/ingress-gce/issues/605)
- Release Summary: v1.4.1 [\#602](https://github.com/kubernetes/ingress-gce/issues/602)
- BackendConfig timeoutSec doesn't seem to work with GCE Ingress [\#598](https://github.com/kubernetes/ingress-gce/issues/598)
- Ingress controller does not work with TCP readiness probe, defaults back to HTTP [\#596](https://github.com/kubernetes/ingress-gce/issues/596)
- Feature Request - Configuration via ConfigMap [\#593](https://github.com/kubernetes/ingress-gce/issues/593)
- Firewall change required by network admin [\#584](https://github.com/kubernetes/ingress-gce/issues/584)
- Not exposed in ingress [\#568](https://github.com/kubernetes/ingress-gce/issues/568)
- URL map / backend service mapping is totally shuffled [\#555](https://github.com/kubernetes/ingress-gce/issues/555)
- Backend health is reported as "Unknown" if there are no pods in the first zone of a regional cluster [\#554](https://github.com/kubernetes/ingress-gce/issues/554)
- Ingress controller does not support HTTP2 with mutual TLS [\#553](https://github.com/kubernetes/ingress-gce/issues/553)
- ErrImagePull: registry.k8s.io/defaultbackend:1.5 not found [\#549](https://github.com/kubernetes/ingress-gce/issues/549)
- configure maxRatePerInstance in backend [\#545](https://github.com/kubernetes/ingress-gce/issues/545)
- GKE BackendConfig permissions change `container.backendConfigs.get` does not exist [\#538](https://github.com/kubernetes/ingress-gce/issues/538)
- A new home for 404-server \(defaultbackend\) [\#498](https://github.com/kubernetes/ingress-gce/issues/498)
- Does not work if workers are in different subnet.  [\#282](https://github.com/kubernetes/ingress-gce/issues/282)
- original http request origin and host headers are overridden [\#179](https://github.com/kubernetes/ingress-gce/issues/179)

**Merged pull requests:**

- Modify NameBelongToCluster to tolerate truncated cluster name suffix [\#650](https://github.com/kubernetes/ingress-gce/pull/650) ([freehan](https://github.com/freehan))
- Shorten the name of the namespace for test sandboxes [\#648](https://github.com/kubernetes/ingress-gce/pull/648) ([rramkumar1](https://github.com/rramkumar1))
- Move lone function in kubeapi.go into existing utils.go [\#644](https://github.com/kubernetes/ingress-gce/pull/644) ([rramkumar1](https://github.com/rramkumar1))
- Update CHANGELOG and version mapping for v1.4.3 [\#643](https://github.com/kubernetes/ingress-gce/pull/643) ([rramkumar1](https://github.com/rramkumar1))
- Support secret-based and pre-shared certs at the same time [\#641](https://github.com/kubernetes/ingress-gce/pull/641) ([michallowicki](https://github.com/michallowicki))
- Remove direct support for ManagedCertificate CRD [\#637](https://github.com/kubernetes/ingress-gce/pull/637) ([michallowicki](https://github.com/michallowicki))
- Update GKE version mapping in README [\#630](https://github.com/kubernetes/ingress-gce/pull/630) ([rramkumar1](https://github.com/rramkumar1))
- Configure leader election with completely separate k8s client [\#623](https://github.com/kubernetes/ingress-gce/pull/623) ([rramkumar1](https://github.com/rramkumar1))
- Simplify upgrade\_test - only loop during k8s master changes. [\#620](https://github.com/kubernetes/ingress-gce/pull/620) ([agau4779](https://github.com/agau4779))
- Add more unit tests for transaction syncer [\#619](https://github.com/kubernetes/ingress-gce/pull/619) ([freehan](https://github.com/freehan))
- make backoff retry handler unit test faster [\#618](https://github.com/kubernetes/ingress-gce/pull/618) ([freehan](https://github.com/freehan))
- Add changelog for 1.4.2 release [\#615](https://github.com/kubernetes/ingress-gce/pull/615) ([agau4779](https://github.com/agau4779))
- Add Finalizers for Ingress [\#613](https://github.com/kubernetes/ingress-gce/pull/613) ([agau4779](https://github.com/agau4779))
- \[BackendConfig\] CDN default cache key policy should be true instead of false. [\#611](https://github.com/kubernetes/ingress-gce/pull/611) ([rramkumar1](https://github.com/rramkumar1))
- standardize loadbalancer naming for GC [\#607](https://github.com/kubernetes/ingress-gce/pull/607) ([agau4779](https://github.com/agau4779))
- Add changelog for v1.4.1 + Add config file for github-changelog-generator [\#604](https://github.com/kubernetes/ingress-gce/pull/604) ([rramkumar1](https://github.com/rramkumar1))
- Add link to OSS Ingress docs in README.md [\#601](https://github.com/kubernetes/ingress-gce/pull/601) ([rramkumar1](https://github.com/rramkumar1))
- Delete documentation that is now covered by GCP docs [\#600](https://github.com/kubernetes/ingress-gce/pull/600) ([rramkumar1](https://github.com/rramkumar1))
- Switch to use the beta+ NEG API [\#597](https://github.com/kubernetes/ingress-gce/pull/597) ([freehan](https://github.com/freehan))
- add unit test for GetZoneForNode helper func [\#594](https://github.com/kubernetes/ingress-gce/pull/594) ([freehan](https://github.com/freehan))
- \[E2E\] In run.sh, add option to disable the resource dump at the end of the test. [\#592](https://github.com/kubernetes/ingress-gce/pull/592) ([rramkumar1](https://github.com/rramkumar1))
- Replace all uses of Snapshotter with CloudLister [\#590](https://github.com/kubernetes/ingress-gce/pull/590) ([agau4779](https://github.com/agau4779))
- Upgrade Test: Ignore connection refused errors when WaitForIngress\(\) is called. [\#588](https://github.com/kubernetes/ingress-gce/pull/588) ([rramkumar1](https://github.com/rramkumar1))
- Revert "Remove unused named ports from instance group's" [\#585](https://github.com/kubernetes/ingress-gce/pull/585) ([rramkumar1](https://github.com/rramkumar1))
- reflect readiness probe in health check for NEG enabled ClusterIP service backend [\#582](https://github.com/kubernetes/ingress-gce/pull/582) ([freehan](https://github.com/freehan))
- fix a bug where transaction neg syncer will miss neg retry  [\#581](https://github.com/kubernetes/ingress-gce/pull/581) ([freehan](https://github.com/freehan))
- remove loop from flush\(\). start/stop informers depending on k8s master [\#580](https://github.com/kubernetes/ingress-gce/pull/580) ([agau4779](https://github.com/agau4779))
- add a flag to control neg syncer type [\#579](https://github.com/kubernetes/ingress-gce/pull/579) ([freehan](https://github.com/freehan))
- update openapi generator. generate v1 backendconfig files [\#578](https://github.com/kubernetes/ingress-gce/pull/578) ([agau4779](https://github.com/agau4779))
- Ingress e2e test cleanup [\#577](https://github.com/kubernetes/ingress-gce/pull/577) ([agau4779](https://github.com/agau4779))
- do not use condition predicate while getting zone for node [\#576](https://github.com/kubernetes/ingress-gce/pull/576) ([pondohva](https://github.com/pondohva))
- Increase deletion timeout for e2e tests [\#575](https://github.com/kubernetes/ingress-gce/pull/575) ([rramkumar1](https://github.com/rramkumar1))
- Fix error format strings according to best practices from CodeReviewComments [\#574](https://github.com/kubernetes/ingress-gce/pull/574) ([CodeLingoBot](https://github.com/CodeLingoBot))
- Fixed misspell \(annotation name\) i README.md [\#573](https://github.com/kubernetes/ingress-gce/pull/573) ([jaceq](https://github.com/jaceq))
- Add masterupgrading flag [\#571](https://github.com/kubernetes/ingress-gce/pull/571) ([agau4779](https://github.com/agau4779))
- Create task queues with a name so that usage metrics can be tracked by Prometheus [\#570](https://github.com/kubernetes/ingress-gce/pull/570) ([rramkumar1](https://github.com/rramkumar1))
- Ingress upgrade testing fixes [\#567](https://github.com/kubernetes/ingress-gce/pull/567) ([agau4779](https://github.com/agau4779))
- rename basic\_upgrade -\> upgrade [\#561](https://github.com/kubernetes/ingress-gce/pull/561) ([agau4779](https://github.com/agau4779))
- Initialize directories which will house our updated documentation. [\#558](https://github.com/kubernetes/ingress-gce/pull/558) ([rramkumar1](https://github.com/rramkumar1))
- defaultbackend image now has the architecture in the image name [\#556](https://github.com/kubernetes/ingress-gce/pull/556) ([bowei](https://github.com/bowei))
- Fix wrong filtering of ManagedCertificate objects. [\#551](https://github.com/kubernetes/ingress-gce/pull/551) ([krzykwas](https://github.com/krzykwas))
- Connection Draining E2E: Fix test log message level [\#548](https://github.com/kubernetes/ingress-gce/pull/548) ([rramkumar1](https://github.com/rramkumar1))
- do not leak LB when ingress class is changed [\#547](https://github.com/kubernetes/ingress-gce/pull/547) ([pondohva](https://github.com/pondohva))
- Ingress Upgrade Testing [\#546](https://github.com/kubernetes/ingress-gce/pull/546) ([agau4779](https://github.com/agau4779))
- Update version mapping to latest state [\#536](https://github.com/kubernetes/ingress-gce/pull/536) ([rramkumar1](https://github.com/rramkumar1))
- README.md: don't assume instance groups where NEG is an option [\#533](https://github.com/kubernetes/ingress-gce/pull/533) ([bpineau](https://github.com/bpineau))
- Transaction Syncer [\#532](https://github.com/kubernetes/ingress-gce/pull/532) ([freehan](https://github.com/freehan))
- add util for transaction table [\#528](https://github.com/kubernetes/ingress-gce/pull/528) ([freehan](https://github.com/freehan))
- e2e and fuzz tests for session affinity [\#527](https://github.com/kubernetes/ingress-gce/pull/527) ([bpineau](https://github.com/bpineau))
- e2e tests for timeout and draining timeout [\#521](https://github.com/kubernetes/ingress-gce/pull/521) ([bpineau](https://github.com/bpineau))
- Add pkg/common/operator & pkg/common/typed to make resource joins much cleaner. [\#517](https://github.com/kubernetes/ingress-gce/pull/517) ([rramkumar1](https://github.com/rramkumar1))
- Add Syncer Skeleton [\#509](https://github.com/kubernetes/ingress-gce/pull/509) ([freehan](https://github.com/freehan))
-  Welcome defaultbackend to the ingress-gce repo [\#503](https://github.com/kubernetes/ingress-gce/pull/503) ([jonpulsifer](https://github.com/jonpulsifer))
- Add a Backoff Handler utils [\#499](https://github.com/kubernetes/ingress-gce/pull/499) ([freehan](https://github.com/freehan))

# Change Log

## [v1.4.3](https://github.com/kubernetes/ingress-gce/tree/v1.4.3) (2019-02-12)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.4.2...v1.4.3)

**Closed issues:**

- GCP - Kubernetes Ingress Backend service unhealthy [\#621](https://github.com/kubernetes/ingress-gce/issues/621)
- BackendConfig security policy not enforced  [\#616](https://github.com/kubernetes/ingress-gce/issues/616)
- original http request origin and host headers are overridden [\#179](https://github.com/kubernetes/ingress-gce/issues/179)

**Merged pull requests:**

- reflect readiness probe in health check for NEG enabled ClusterIP service backend \[release-1.4\] [\#633](https://github.com/kubernetes/ingress-gce/pull/633) ([freehan](https://github.com/freehan))
- do not leak LB when ingress class is changed \[release-1.4\] [\#626](https://github.com/kubernetes/ingress-gce/pull/626) ([agau4779](https://github.com/agau4779))
- Configure leader election with completely separate k8s client \[release-1.4\] [\#624](https://github.com/kubernetes/ingress-gce/pull/624) ([rramkumar1](https://github.com/rramkumar1))

# Change Log

## [v1.4.2](https://github.com/kubernetes/ingress-gce/tree/v1.4.2) (2019-01-16)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.4.1...v1.4.2)

**Implemented enhancements:**

- Certificate update path is not clear [\#525](https://github.com/kubernetes/ingress-gce/issues/525)

**Fixed bugs:**

- BackendConfig `cdn: enable: true` uses scary defaults [\#599](https://github.com/kubernetes/ingress-gce/issues/599)

**Closed issues:**

- Multiple e2e jobs are failing [\#606](https://github.com/kubernetes/ingress-gce/issues/606)
- GKE ingress stuck in creating after deploying ingress 1.11.5.gke.5 [\#605](https://github.com/kubernetes/ingress-gce/issues/605)
- Release Summary: v1.4.1 [\#602](https://github.com/kubernetes/ingress-gce/issues/602)
- BackendConfig timeoutSec doesn't seem to work with GCE Ingress [\#598](https://github.com/kubernetes/ingress-gce/issues/598)
- Ingress controller does not work with TCP readiness probe, defaults back to HTTP [\#596](https://github.com/kubernetes/ingress-gce/issues/596)
- Feature Request - Configuration via ConfigMap [\#593](https://github.com/kubernetes/ingress-gce/issues/593)
- Firewall change required by network admin [\#584](https://github.com/kubernetes/ingress-gce/issues/584)
- Not exposed in ingress [\#568](https://github.com/kubernetes/ingress-gce/issues/568)

**Merged pull requests:**

- \(1.4 Cherry Pick\) \[BackendConfig\] CDN default cache key policy should be true instead of false [\#612](https://github.com/kubernetes/ingress-gce/pull/612) ([rramkumar1](https://github.com/rramkumar1))
- do not use condition predicate while getting zone for node [\#609](https://github.com/kubernetes/ingress-gce/pull/609) ([freehan](https://github.com/freehan))

# Change Log

## [v1.4.1](https://github.com/kubernetes/ingress-gce/tree/v1.4.1) (2019-01-04)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.4.0...v1.4.1)

**Fixed bugs:**

- Instance is Not Removed from IG when node is marked as unschedulable [\#591](https://github.com/kubernetes/ingress-gce/issues/591)
- Readiness Probe does not get reflected for NEG enabled ingress [\#541](https://github.com/kubernetes/ingress-gce/issues/541)
- Firewall rule required message ignores existing rules \(shared VPC\) [\#485](https://github.com/kubernetes/ingress-gce/issues/485)
- Ingress-GCE does not GC LB resources when ingress class changed and no other ingress on GCE exists [\#481](https://github.com/kubernetes/ingress-gce/issues/481)
- ingress controller gave me 2 IP addresses instead of 1 when I added TLS [\#410](https://github.com/kubernetes/ingress-gce/issues/410)

**Closed issues:**

- URL map / backend service mapping is totally shuffled [\#555](https://github.com/kubernetes/ingress-gce/issues/555)
- Backend health is reported as "Unknown" if there are no pods in the first zone of a regional cluster [\#554](https://github.com/kubernetes/ingress-gce/issues/554)
- Ingress controller does not support HTTP2 with mutual TLS [\#553](https://github.com/kubernetes/ingress-gce/issues/553)
- ErrImagePull: registry.k8s.io/defaultbackend:1.5 not found [\#549](https://github.com/kubernetes/ingress-gce/issues/549)
- configure maxRatePerInstance in backend [\#545](https://github.com/kubernetes/ingress-gce/issues/545)
- GKE BackendConfig permissions change `container.backendConfigs.get` does not exist [\#538](https://github.com/kubernetes/ingress-gce/issues/538)
- A new home for 404-server \(defaultbackend\) [\#498](https://github.com/kubernetes/ingress-gce/issues/498)
- Does not work if workers are in different subnet.  [\#282](https://github.com/kubernetes/ingress-gce/issues/282)

**Merged pull requests:**

- \[1.4 Cherry pick\] Revert "Remove unused named ports from instance group's" [\#587](https://github.com/kubernetes/ingress-gce/pull/587) ([rramkumar1](https://github.com/rramkumar1))
- Fix wrong filtering of ManagedCertificate objects. [\#552](https://github.com/kubernetes/ingress-gce/pull/552) ([krzykwas](https://github.com/krzykwas))

# Change Log

## [Unreleased](https://github.com/kubernetes/ingress-gce/tree/HEAD)

[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.4.0...HEAD)

**Closed issues:**

- Rewrite ingress-gce general documentation [\#249](https://github.com/kubernetes/ingress-gce/issues/249)
- Documentation: Would like to see example yaml snippet for GCP certificate resource for SSL/TLS [\#229](https://github.com/kubernetes/ingress-gce/issues/229)
- GCE health check does not pick up changes to pod readinessProbe [\#39](https://github.com/kubernetes/ingress-gce/issues/39)
- \[GLBC\] Expose GCE backend parameters in Ingress object API [\#28](https://github.com/kubernetes/ingress-gce/issues/28)

## [v1.4.0](https://github.com/kubernetes/ingress-gce/tree/v1.4.0) (2018-10-30)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.3.3...v1.4.0)

**Closed issues:**

- can not access  nginx through ingress and problem in curl  [\#490](https://github.com/kubernetes/ingress-gce/issues/490)
- HTTP2 health check does not use readiness probe path [\#487](https://github.com/kubernetes/ingress-gce/issues/487)
- Tool or job to cleanup ingress related GCP resource after test failure [\#483](https://github.com/kubernetes/ingress-gce/issues/483)
- Changes to ingress resource doesn't update forwarding rules most of the time in 1.10.6-gke.2 [\#477](https://github.com/kubernetes/ingress-gce/issues/477)
- GKE Ingress controller ignoring ingress.class annotation [\#476](https://github.com/kubernetes/ingress-gce/issues/476)
- Addon-manager "Reconcile" annotation deletes default-http-backend service and deployment [\#474](https://github.com/kubernetes/ingress-gce/issues/474)
- Ingress-GCE has a nil pointer exception [\#471](https://github.com/kubernetes/ingress-gce/issues/471)
- GKE ingress with https load balancer and IAP/security policy enabled [\#469](https://github.com/kubernetes/ingress-gce/issues/469)
- Allow configuration-snippet Annotation. [\#445](https://github.com/kubernetes/ingress-gce/issues/445)
- ci-ingress-gce-e2e-scale is failing [\#438](https://github.com/kubernetes/ingress-gce/issues/438)
- unhealthy backend services, with 400, 412, 409 errors  [\#396](https://github.com/kubernetes/ingress-gce/issues/396)
- Need e2e test to ensure an ingress with non-nodeport services won't break the others [\#250](https://github.com/kubernetes/ingress-gce/issues/250)
- Fail earlier instead of using defaults [\#201](https://github.com/kubernetes/ingress-gce/issues/201)
- Ingress Controller Clobbers Backend Service [\#156](https://github.com/kubernetes/ingress-gce/issues/156)
- Test ingress creation after hitting error creating GCP resource due to quota [\#94](https://github.com/kubernetes/ingress-gce/issues/94)

**Merged pull requests:**

- Managed Certs integration: Fix code so that informers are not instantiated if managed certs is not enabled [\#529](https://github.com/kubernetes/ingress-gce/pull/529) ([rramkumar1](https://github.com/rramkumar1))
- BackendConfig support for session affinity [\#526](https://github.com/kubernetes/ingress-gce/pull/526) ([bpineau](https://github.com/bpineau))
- Add an example for running specific test case to readme [\#524](https://github.com/kubernetes/ingress-gce/pull/524) ([MrHohn](https://github.com/MrHohn))
- In e2e tests, always skip checking for deletion of default backend service [\#523](https://github.com/kubernetes/ingress-gce/pull/523) ([rramkumar1](https://github.com/rramkumar1))
- \[e2e test\] append key value to resources instead of pointer [\#522](https://github.com/kubernetes/ingress-gce/pull/522) ([MrHohn](https://github.com/MrHohn))
- Update deploy/glbc yaml files for BackendConfig [\#520](https://github.com/kubernetes/ingress-gce/pull/520) ([bpineau](https://github.com/bpineau))
- Fix events-based e2e tests [\#519](https://github.com/kubernetes/ingress-gce/pull/519) ([bpineau](https://github.com/bpineau))
- BackendConfig support for timeouts and connection draining [\#513](https://github.com/kubernetes/ingress-gce/pull/513) ([bpineau](https://github.com/bpineau))
- Implement support for ManagedCertificate CRD [\#508](https://github.com/kubernetes/ingress-gce/pull/508) ([krzykwas](https://github.com/krzykwas))
- Allow for setting the rate limiter on the work queue [\#505](https://github.com/kubernetes/ingress-gce/pull/505) ([rramkumar1](https://github.com/rramkumar1))
- Expose utils.hasFinalizer [\#504](https://github.com/kubernetes/ingress-gce/pull/504) ([rramkumar1](https://github.com/rramkumar1))
- Fix a potential nil pointer exception [\#502](https://github.com/kubernetes/ingress-gce/pull/502) ([freehan](https://github.com/freehan))
- merge negBelongsToCluster into IsNEG [\#501](https://github.com/kubernetes/ingress-gce/pull/501) ([freehan](https://github.com/freehan))
- Update OWNERS file to reflect reality. [\#500](https://github.com/kubernetes/ingress-gce/pull/500) ([rramkumar1](https://github.com/rramkumar1))
- Update vendor [\#497](https://github.com/kubernetes/ingress-gce/pull/497) ([rramkumar1](https://github.com/rramkumar1))
- Update defaultbackend image to 1.5 [\#496](https://github.com/kubernetes/ingress-gce/pull/496) ([aledbf](https://github.com/aledbf))
- include NEG naming scheme for NameBelongsToCluster [\#494](https://github.com/kubernetes/ingress-gce/pull/494) ([freehan](https://github.com/freehan))
- Restructure syncer package [\#492](https://github.com/kubernetes/ingress-gce/pull/492) ([freehan](https://github.com/freehan))
- Modify GroupKey type to contain group name + modify NEG linker to consider provided name [\#491](https://github.com/kubernetes/ingress-gce/pull/491) ([rramkumar1](https://github.com/rramkumar1))
- Restructure NEG controller [\#489](https://github.com/kubernetes/ingress-gce/pull/489) ([freehan](https://github.com/freehan))
- Use HTTPS readiness probe for HTTP2 services because the Kubernetes A… [\#488](https://github.com/kubernetes/ingress-gce/pull/488) ([anuraaga](https://github.com/anuraaga))
- Bump Alpine base image version [\#484](https://github.com/kubernetes/ingress-gce/pull/484) ([awly](https://github.com/awly))
- add work queue to process endpoint changes [\#482](https://github.com/kubernetes/ingress-gce/pull/482) ([freehan](https://github.com/freehan))
- Replace kubernetes-users mailing list link with discuss forum link [\#479](https://github.com/kubernetes/ingress-gce/pull/479) ([mrbobbytables](https://github.com/mrbobbytables))
- Remove add on manager label from GLBC Yaml deployment [\#478](https://github.com/kubernetes/ingress-gce/pull/478) ([rramkumar1](https://github.com/rramkumar1))
- Fix the issue where Shutdown doesn't shutdown taskqueue [\#365](https://github.com/kubernetes/ingress-gce/pull/365) ([anfernee](https://github.com/anfernee))

## [v1.3.3](https://github.com/kubernetes/ingress-gce/tree/v1.3.3) (2018-09-13)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.3.2...v1.3.3)

**Closed issues:**

- GCE ingress stuck on "Creating ingress" status, existing ingresses don't update [\#470](https://github.com/kubernetes/ingress-gce/issues/470)
- Issue with multiple domains and SSL certificates when using ingress-gce [\#466](https://github.com/kubernetes/ingress-gce/issues/466)

**Merged pull requests:**

- Cherrypick \#388 on release 1.3 branch [\#473](https://github.com/kubernetes/ingress-gce/pull/473) ([rramkumar1](https://github.com/rramkumar1))
- Cherrypick of \#434 on release 1.3 [\#472](https://github.com/kubernetes/ingress-gce/pull/472) ([rramkumar1](https://github.com/rramkumar1))
- Revert "Refactor some uses of snapshotter.Add\(\) to use bool rather than real object" [\#464](https://github.com/kubernetes/ingress-gce/pull/464) ([rramkumar1](https://github.com/rramkumar1))
- Support short names in CRD Meta. Allows for abbreviating CRD's in kubectl [\#463](https://github.com/kubernetes/ingress-gce/pull/463) ([rramkumar1](https://github.com/rramkumar1))
- Harden NEG GC [\#459](https://github.com/kubernetes/ingress-gce/pull/459) ([freehan](https://github.com/freehan))
- Do not truncate tls certs based on target proxy limit [\#451](https://github.com/kubernetes/ingress-gce/pull/451) ([prameshj](https://github.com/prameshj))
- Fire warning event instead of hard failing if TLS certificate is not present [\#388](https://github.com/kubernetes/ingress-gce/pull/388) ([munnerz](https://github.com/munnerz))

## [v1.3.2](https://github.com/kubernetes/ingress-gce/tree/v1.3.2) (2018-08-31)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.3.1...v1.3.2)

**Merged pull requests:**

- Cherrypick \#461 into release 1.3 [\#462](https://github.com/kubernetes/ingress-gce/pull/462) ([freehan](https://github.com/freehan))
- Refactor Ingress Filtering and Ingress Backend Traversal [\#461](https://github.com/kubernetes/ingress-gce/pull/461) ([freehan](https://github.com/freehan))
- Refactor some uses of snapshotter.Add\(\) to use bool rather than real object [\#458](https://github.com/kubernetes/ingress-gce/pull/458) ([rramkumar1](https://github.com/rramkumar1))
- Cherrypick \#456 into release-1.3 [\#457](https://github.com/kubernetes/ingress-gce/pull/457) ([freehan](https://github.com/freehan))
- NEG controller bug fix [\#456](https://github.com/kubernetes/ingress-gce/pull/456) ([freehan](https://github.com/freehan))
- Remove JSONMergePatch util [\#455](https://github.com/kubernetes/ingress-gce/pull/455) ([rramkumar1](https://github.com/rramkumar1))

## [v1.3.1](https://github.com/kubernetes/ingress-gce/tree/v1.3.1) (2018-08-29)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.3.0...v1.3.1)

**Closed issues:**

- When using NEG services only, the controller still creates instance groups [\#433](https://github.com/kubernetes/ingress-gce/issues/433)
- \[GLBC\] LB garbage collection orphans named ports in instance groups [\#43](https://github.com/kubernetes/ingress-gce/issues/43)

**Merged pull requests:**

- Cherrypick \#452 into release-1.3 [\#454](https://github.com/kubernetes/ingress-gce/pull/454) ([freehan](https://github.com/freehan))
- Add JSONMerge patch utilities and move some files around [\#453](https://github.com/kubernetes/ingress-gce/pull/453) ([rramkumar1](https://github.com/rramkumar1))
- fix and refactor on NEG annotation handling [\#452](https://github.com/kubernetes/ingress-gce/pull/452) ([freehan](https://github.com/freehan))
- deflake TestGetNodePortsUsedByIngress unit test [\#450](https://github.com/kubernetes/ingress-gce/pull/450) ([freehan](https://github.com/freehan))
- Move joinErrs\(\) to utils and export it [\#449](https://github.com/kubernetes/ingress-gce/pull/449) ([rramkumar1](https://github.com/rramkumar1))
- Slight refactor of Controller interface to eliminate Ingress type specifically [\#448](https://github.com/kubernetes/ingress-gce/pull/448) ([rramkumar1](https://github.com/rramkumar1))
- Export getPatchBytes in pkg/util [\#447](https://github.com/kubernetes/ingress-gce/pull/447) ([rramkumar1](https://github.com/rramkumar1))
- Add some utility functions to support finalizers [\#446](https://github.com/kubernetes/ingress-gce/pull/446) ([rramkumar1](https://github.com/rramkumar1))
- Cherrypick \#442 into release-1.3 [\#444](https://github.com/kubernetes/ingress-gce/pull/444) ([MrHohn](https://github.com/MrHohn))
- Move NegStatus and PortNameMap to pkg/neg/types [\#443](https://github.com/kubernetes/ingress-gce/pull/443) ([rramkumar1](https://github.com/rramkumar1))
- Remove 'Description' and 'Required' from OpenAPI schema root layer [\#442](https://github.com/kubernetes/ingress-gce/pull/442) ([MrHohn](https://github.com/MrHohn))
- Fix main controller health check [\#441](https://github.com/kubernetes/ingress-gce/pull/441) ([rramkumar1](https://github.com/rramkumar1))
- do not create instance group with NEG backends [\#440](https://github.com/kubernetes/ingress-gce/pull/440) ([freehan](https://github.com/freehan))
- Remove unused named ports from instance group's  [\#430](https://github.com/kubernetes/ingress-gce/pull/430) ([rramkumar1](https://github.com/rramkumar1))
- Introduce a new interface to encapsulate Ingress sync and controller implementation of the sync [\#428](https://github.com/kubernetes/ingress-gce/pull/428) ([rramkumar1](https://github.com/rramkumar1))

## [v1.3.0](https://github.com/kubernetes/ingress-gce/tree/v1.3.0) (2018-08-16)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.2.3...v1.3.0)

**Closed issues:**

- ingress-gce-image-push job is failing [\#426](https://github.com/kubernetes/ingress-gce/issues/426)
- Multiple pre-shared certificates not working [\#419](https://github.com/kubernetes/ingress-gce/issues/419)
- BackendService Sync bug in 1.2 [\#400](https://github.com/kubernetes/ingress-gce/issues/400)
- Way to check what version of GLBC is running on a GKE cluster [\#395](https://github.com/kubernetes/ingress-gce/issues/395)
- Align node filtering with service controller [\#292](https://github.com/kubernetes/ingress-gce/issues/292)
- Remove NEG FeatureGate [\#274](https://github.com/kubernetes/ingress-gce/issues/274)
- Explore options for removing code which adds legacy GCE health check settings  [\#269](https://github.com/kubernetes/ingress-gce/issues/269)
- Clean up unit tests for load balancer pool, backend pool and controller [\#261](https://github.com/kubernetes/ingress-gce/issues/261)

**Merged pull requests:**

- Cherry-pick necessary commits for v1.3.0 release. [\#439](https://github.com/kubernetes/ingress-gce/pull/439) ([rramkumar1](https://github.com/rramkumar1))
- Add myself to OWNERS so I can do a release. [\#437](https://github.com/kubernetes/ingress-gce/pull/437) ([rramkumar1](https://github.com/rramkumar1))
- Fix error handling in controller sync\(\) [\#436](https://github.com/kubernetes/ingress-gce/pull/436) ([rramkumar1](https://github.com/rramkumar1))
- BackendPool Update\(\) should set backend service version [\#435](https://github.com/kubernetes/ingress-gce/pull/435) ([rramkumar1](https://github.com/rramkumar1))
- Fix null-pointer exception in url map ensure logic [\#434](https://github.com/kubernetes/ingress-gce/pull/434) ([rramkumar1](https://github.com/rramkumar1))
- Add a version mapping for both GCE and GKE clusters [\#432](https://github.com/kubernetes/ingress-gce/pull/432) ([rramkumar1](https://github.com/rramkumar1))
- Fix bug in backend syncer where backend service was being created without health check [\#431](https://github.com/kubernetes/ingress-gce/pull/431) ([rramkumar1](https://github.com/rramkumar1))
- move NewIndexer to utils [\#429](https://github.com/kubernetes/ingress-gce/pull/429) ([agau4779](https://github.com/agau4779))
- Push dependency on the Cloud up out of the neg controller [\#425](https://github.com/kubernetes/ingress-gce/pull/425) ([bowei](https://github.com/bowei))
- Extract BackendPool interface into three 3 separate interfaces [\#424](https://github.com/kubernetes/ingress-gce/pull/424) ([rramkumar1](https://github.com/rramkumar1))
- export TrimFieldsEvenly [\#423](https://github.com/kubernetes/ingress-gce/pull/423) ([agau4779](https://github.com/agau4779))
- Refactor to remove ClusterManager completely  [\#422](https://github.com/kubernetes/ingress-gce/pull/422) ([rramkumar1](https://github.com/rramkumar1))
- Remove EnsureInstanceGroupsAndPorts wrapper func [\#421](https://github.com/kubernetes/ingress-gce/pull/421) ([rramkumar1](https://github.com/rramkumar1))
- Move joiner methods into context [\#420](https://github.com/kubernetes/ingress-gce/pull/420) ([rramkumar1](https://github.com/rramkumar1))
- Remove all code related to legacy health checks [\#418](https://github.com/kubernetes/ingress-gce/pull/418) ([rramkumar1](https://github.com/rramkumar1))
- Introduce cloud.google.com/app-protocols to eventually replace existing annotation [\#417](https://github.com/kubernetes/ingress-gce/pull/417) ([rramkumar1](https://github.com/rramkumar1))
- Add doc link for backend config [\#416](https://github.com/kubernetes/ingress-gce/pull/416) ([MrHohn](https://github.com/MrHohn))
- Expose newIndexer in pkg/context [\#415](https://github.com/kubernetes/ingress-gce/pull/415) ([rramkumar1](https://github.com/rramkumar1))
- unit test: add locking when read from shared map [\#414](https://github.com/kubernetes/ingress-gce/pull/414) ([MrHohn](https://github.com/MrHohn))
- Rename context's hcLock to lock [\#413](https://github.com/kubernetes/ingress-gce/pull/413) ([rramkumar1](https://github.com/rramkumar1))
- Bump timeout in tests to match reality [\#412](https://github.com/kubernetes/ingress-gce/pull/412) ([MrHohn](https://github.com/MrHohn))
- Fix issue with yaml in glbc deploy/ [\#411](https://github.com/kubernetes/ingress-gce/pull/411) ([rramkumar1](https://github.com/rramkumar1))
- Extract firewall management into separate controller [\#403](https://github.com/kubernetes/ingress-gce/pull/403) ([rramkumar1](https://github.com/rramkumar1))
- Update README.md [\#391](https://github.com/kubernetes/ingress-gce/pull/391) ([mgub](https://github.com/mgub))
- Align node filtering with kubernetes service controller [\#370](https://github.com/kubernetes/ingress-gce/pull/370) ([lbernail](https://github.com/lbernail))

## [v1.2.3](https://github.com/kubernetes/ingress-gce/tree/v1.2.3) (2018-07-19)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.2.2...v1.2.3)

**Closed issues:**

- External CNAME records do not route properly via hostnames [\#404](https://github.com/kubernetes/ingress-gce/issues/404)
- Add annotation for specifying backend-service timeout [\#399](https://github.com/kubernetes/ingress-gce/issues/399)
- Unneeded health check created for kube-system:default-http-backend service  [\#385](https://github.com/kubernetes/ingress-gce/issues/385)
- Ingress health check not following ReadinessProbe [\#317](https://github.com/kubernetes/ingress-gce/issues/317)
- Fix the coverage badge [\#132](https://github.com/kubernetes/ingress-gce/issues/132)
- Does/will the GCE ingress controller support whitelist-source-range? [\#38](https://github.com/kubernetes/ingress-gce/issues/38)

**Merged pull requests:**

- Cherry pick on release-1.2 for \#408 [\#409](https://github.com/kubernetes/ingress-gce/pull/409) ([rramkumar1](https://github.com/rramkumar1))
- Raw patch to cloud provider to fix operations issue [\#408](https://github.com/kubernetes/ingress-gce/pull/408) ([rramkumar1](https://github.com/rramkumar1))
- cherrypick \#402 to release-1.2 branch [\#407](https://github.com/kubernetes/ingress-gce/pull/407) ([freehan](https://github.com/freehan))
- Remove EnsureLB from ClusterManager [\#406](https://github.com/kubernetes/ingress-gce/pull/406) ([rramkumar1](https://github.com/rramkumar1))
- Introduce ControllerContextConfig and move some command-line tunable stuff there [\#405](https://github.com/kubernetes/ingress-gce/pull/405) ([rramkumar1](https://github.com/rramkumar1))
- uniq function should compare more than NodePort difference [\#402](https://github.com/kubernetes/ingress-gce/pull/402) ([freehan](https://github.com/freehan))
- Pace operation polling [\#401](https://github.com/kubernetes/ingress-gce/pull/401) ([nicksardo](https://github.com/nicksardo))
- Remove error return value from controller initialization [\#398](https://github.com/kubernetes/ingress-gce/pull/398) ([rramkumar1](https://github.com/rramkumar1))
- Documentation fixes [\#394](https://github.com/kubernetes/ingress-gce/pull/394) ([rramkumar1](https://github.com/rramkumar1))
- Implement security policy validator for real [\#393](https://github.com/kubernetes/ingress-gce/pull/393) ([MrHohn](https://github.com/MrHohn))
- promote http2 to beta [\#382](https://github.com/kubernetes/ingress-gce/pull/382) ([agau4779](https://github.com/agau4779))
- Typo in message: SyncNetworkEndpointGroupFailed-\>SyncNetworkEndpointGroupFailed [\#374](https://github.com/kubernetes/ingress-gce/pull/374) ([AdamDang](https://github.com/AdamDang))
- URLMap sync [\#356](https://github.com/kubernetes/ingress-gce/pull/356) ([nicksardo](https://github.com/nicksardo))

## [v1.2.2](https://github.com/kubernetes/ingress-gce/tree/v1.2.2) (2018-07-09)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.1.2...v1.2.2)

**Closed issues:**

- "./deploy/glbc/script.sh --clean" does not reset the file "./deploy/glbc/yaml/default-http-backend.yaml" [\#363](https://github.com/kubernetes/ingress-gce/issues/363)
- Ingress tries to create SSL certificate from secret with illegal name [\#321](https://github.com/kubernetes/ingress-gce/issues/321)
- tls secrets not updating due to invalid resource.name [\#311](https://github.com/kubernetes/ingress-gce/issues/311)
- Invalid certificate leads to firewall rule no longer being updated on GKE [\#298](https://github.com/kubernetes/ingress-gce/issues/298)
- Exclude Master and ExcludeBalancer nodes from Instance Groups [\#295](https://github.com/kubernetes/ingress-gce/issues/295)
- Error 400: The SSL certificate could not be parsed. [\#294](https://github.com/kubernetes/ingress-gce/issues/294)
- https-only GKE ingress is still showing port 80 [\#290](https://github.com/kubernetes/ingress-gce/issues/290)
- Create a SECURITY\_CONTACTS file. [\#286](https://github.com/kubernetes/ingress-gce/issues/286)
- Ingress e2e tests should ensure that default backend passes health checks. [\#263](https://github.com/kubernetes/ingress-gce/issues/263)
- Long time-to-first-byte problem [\#245](https://github.com/kubernetes/ingress-gce/issues/245)
- Failing to pick up health check from readiness probe [\#241](https://github.com/kubernetes/ingress-gce/issues/241)
- Scale test failed because ingress wasn't deleted [\#219](https://github.com/kubernetes/ingress-gce/issues/219)
- Replace link parsing and generation with cloud library [\#215](https://github.com/kubernetes/ingress-gce/issues/215)
- Support Leader Election for GLBC for HA masters [\#204](https://github.com/kubernetes/ingress-gce/issues/204)
- Failing to create an Ingress shouldn't stop the controller from creating the others [\#197](https://github.com/kubernetes/ingress-gce/issues/197)
- Controller doesnt reenque on instance group GC failure [\#186](https://github.com/kubernetes/ingress-gce/issues/186)
- Condense backend pool with default backend pool [\#184](https://github.com/kubernetes/ingress-gce/issues/184)
- 502 Server Error [\#164](https://github.com/kubernetes/ingress-gce/issues/164)
- Seems ingress don't transfer the "Transfer-encoding" Header from backend [\#157](https://github.com/kubernetes/ingress-gce/issues/157)
- ingress-gce does not work...very little documentation ..instructions lead to 404 pages [\#149](https://github.com/kubernetes/ingress-gce/issues/149)
- REST request fails with "The server encountered a temporary error and could not complete your request. Please try again in 30 seconds" [\#141](https://github.com/kubernetes/ingress-gce/issues/141)
- SSL certificate name non-unique when namespace + ingress name too long [\#131](https://github.com/kubernetes/ingress-gce/issues/131)
- Default backend service is created when no ingress needs it [\#127](https://github.com/kubernetes/ingress-gce/issues/127)
- Experiencing downtime when updating hosts backend in ingress controller [\#116](https://github.com/kubernetes/ingress-gce/issues/116)
- ingress path confusing [\#76](https://github.com/kubernetes/ingress-gce/issues/76)
- Is there nginx-controller like session affinity support [\#60](https://github.com/kubernetes/ingress-gce/issues/60)
- multiple TLS certs are not correctly handled by GCE \(no SNI support\) [\#46](https://github.com/kubernetes/ingress-gce/issues/46)
- controllers/gce/README.md doc review [\#45](https://github.com/kubernetes/ingress-gce/issues/45)
- Why does GCE ingress defer promoting static IP [\#37](https://github.com/kubernetes/ingress-gce/issues/37)
- GCE: improve default backend handling [\#23](https://github.com/kubernetes/ingress-gce/issues/23)
- Ingress creates wrong firewall rule after `default-http-backend` service was clobbered. [\#19](https://github.com/kubernetes/ingress-gce/issues/19)

**Merged pull requests:**

- Cherrypick \#383: Unmask get backend config errors [\#390](https://github.com/kubernetes/ingress-gce/pull/390) ([MrHohn](https://github.com/MrHohn))
- Cherrypick \#381and \#384 into release-1.2 [\#389](https://github.com/kubernetes/ingress-gce/pull/389) ([freehan](https://github.com/freehan))
- Modify security policy e2e test to create unique GCP resources. [\#387](https://github.com/kubernetes/ingress-gce/pull/387) ([rramkumar1](https://github.com/rramkumar1))
- Increase timeout on waiting for GLBC resource deletion [\#386](https://github.com/kubernetes/ingress-gce/pull/386) ([rramkumar1](https://github.com/rramkumar1))
- Patch NEG version into features.go and add more docs for features package [\#384](https://github.com/kubernetes/ingress-gce/pull/384) ([MrHohn](https://github.com/MrHohn))
- Add more negative test cases for backend config [\#383](https://github.com/kubernetes/ingress-gce/pull/383) ([MrHohn](https://github.com/MrHohn))
- Some fixes for 1.2 [\#381](https://github.com/kubernetes/ingress-gce/pull/381) ([freehan](https://github.com/freehan))
- cherrypick \#377 into release-1.2 [\#378](https://github.com/kubernetes/ingress-gce/pull/378) ([freehan](https://github.com/freehan))
- PortNameMap should also compare values [\#377](https://github.com/kubernetes/ingress-gce/pull/377) ([freehan](https://github.com/freehan))
- Fix run.sh to properly print exit code of test run [\#376](https://github.com/kubernetes/ingress-gce/pull/376) ([rramkumar1](https://github.com/rramkumar1))
- Add a negative test case for referencing not exist BackendConfig [\#372](https://github.com/kubernetes/ingress-gce/pull/372) ([MrHohn](https://github.com/MrHohn))
- Fix WaitForGCLBDeletion\(\) callers [\#371](https://github.com/kubernetes/ingress-gce/pull/371) ([MrHohn](https://github.com/MrHohn))
- Update deploy script to edit copy of default backend service yaml [\#368](https://github.com/kubernetes/ingress-gce/pull/368) ([rramkumar1](https://github.com/rramkumar1))
- Add simple e2e test for CDN & IAP  [\#367](https://github.com/kubernetes/ingress-gce/pull/367) ([rramkumar1](https://github.com/rramkumar1))
- Switch to use beta HealthCheck for NEG [\#366](https://github.com/kubernetes/ingress-gce/pull/366) ([freehan](https://github.com/freehan))
- Fix order-dependency in test cases [\#364](https://github.com/kubernetes/ingress-gce/pull/364) ([anfernee](https://github.com/anfernee))
- Revendor GCE go client, cloud provider and fixes to make it work [\#362](https://github.com/kubernetes/ingress-gce/pull/362) ([freehan](https://github.com/freehan))
- Fix missing gcloud command in e2e script [\#361](https://github.com/kubernetes/ingress-gce/pull/361) ([bowei](https://github.com/bowei))
- Implement e2e test for security policy [\#360](https://github.com/kubernetes/ingress-gce/pull/360) ([MrHohn](https://github.com/MrHohn))
- nit fixes [\#358](https://github.com/kubernetes/ingress-gce/pull/358) ([freehan](https://github.com/freehan))
- Implement fuzzer for feature security policy [\#357](https://github.com/kubernetes/ingress-gce/pull/357) ([MrHohn](https://github.com/MrHohn))
- Add host to echo dump [\#355](https://github.com/kubernetes/ingress-gce/pull/355) ([nicksardo](https://github.com/nicksardo))
- Fix hasAlphaResource and hasBetaResource [\#354](https://github.com/kubernetes/ingress-gce/pull/354) ([MrHohn](https://github.com/MrHohn))
- Add backendconfig client to e2e framework [\#353](https://github.com/kubernetes/ingress-gce/pull/353) ([MrHohn](https://github.com/MrHohn))
- Trigger ingress sync on system default backend update [\#352](https://github.com/kubernetes/ingress-gce/pull/352) ([MrHohn](https://github.com/MrHohn))
- Minor fix to backend config errors [\#351](https://github.com/kubernetes/ingress-gce/pull/351) ([MrHohn](https://github.com/MrHohn))
- merge Ingress NEG annotation and Expose NEG annotation [\#350](https://github.com/kubernetes/ingress-gce/pull/350) ([agau4779](https://github.com/agau4779))
- Add Liveness Probe for NEG controller [\#349](https://github.com/kubernetes/ingress-gce/pull/349) ([freehan](https://github.com/freehan))
- Make sure we get a BackendService after updating it to populate object fingerprint \[WIP\] [\#348](https://github.com/kubernetes/ingress-gce/pull/348) ([rramkumar1](https://github.com/rramkumar1))
- On removal of backend config name from service annotation, ensure no existing settings are affected [\#347](https://github.com/kubernetes/ingress-gce/pull/347) ([rramkumar1](https://github.com/rramkumar1))
- Adds readme for e2e-tests [\#346](https://github.com/kubernetes/ingress-gce/pull/346) ([bowei](https://github.com/bowei))
- Modify IAP + CDN support to not touch settings if section in spec is missing [\#345](https://github.com/kubernetes/ingress-gce/pull/345) ([rramkumar1](https://github.com/rramkumar1))
- Delete ingress and wait for resource deletion [\#344](https://github.com/kubernetes/ingress-gce/pull/344) ([bowei](https://github.com/bowei))
- Retry on getting PROJECT, dump out project resources [\#343](https://github.com/kubernetes/ingress-gce/pull/343) ([bowei](https://github.com/bowei))
- Minor fix for retrieving backendService resource [\#342](https://github.com/kubernetes/ingress-gce/pull/342) ([MrHohn](https://github.com/MrHohn))
- use flag instead of gate for NEG [\#341](https://github.com/kubernetes/ingress-gce/pull/341) ([agau4779](https://github.com/agau4779))
- Testing improvements [\#339](https://github.com/kubernetes/ingress-gce/pull/339) ([rramkumar1](https://github.com/rramkumar1))
- Add logging to the GLBCFromVIP for debugging [\#338](https://github.com/kubernetes/ingress-gce/pull/338) ([bowei](https://github.com/bowei))
- Allow LoadBalancer services [\#335](https://github.com/kubernetes/ingress-gce/pull/335) ([nicksardo](https://github.com/nicksardo))
- Add quotes to echo, allow CONTAINER\_BINARIES override [\#334](https://github.com/kubernetes/ingress-gce/pull/334) ([nicksardo](https://github.com/nicksardo))
- Update Dockerfile for the e2e test [\#333](https://github.com/kubernetes/ingress-gce/pull/333) ([bowei](https://github.com/bowei))
- NEG Metrics [\#332](https://github.com/kubernetes/ingress-gce/pull/332) ([freehan](https://github.com/freehan))
- Add fixtures and helpers in e2e framework for BackendConfig [\#331](https://github.com/kubernetes/ingress-gce/pull/331) ([rramkumar1](https://github.com/rramkumar1))
- Handle empty cluster name in sslcert namer [\#330](https://github.com/kubernetes/ingress-gce/pull/330) ([prameshj](https://github.com/prameshj))
- Fixes [\#329](https://github.com/kubernetes/ingress-gce/pull/329) ([bowei](https://github.com/bowei))
- Make Ingress builder reusable [\#327](https://github.com/kubernetes/ingress-gce/pull/327) ([bowei](https://github.com/bowei))
- Fix typos in copyright year [\#326](https://github.com/kubernetes/ingress-gce/pull/326) ([bowei](https://github.com/bowei))
- Many small cleanups to get basic\_test.go working [\#325](https://github.com/kubernetes/ingress-gce/pull/325) ([bowei](https://github.com/bowei))
- Fix build to only build the executable target [\#324](https://github.com/kubernetes/ingress-gce/pull/324) ([bowei](https://github.com/bowei))
- Add error types for errors to improve testing and readability [\#323](https://github.com/kubernetes/ingress-gce/pull/323) ([rramkumar1](https://github.com/rramkumar1))
- Fix ingress translation logic to not GC backend if non-fatal error occurred [\#322](https://github.com/kubernetes/ingress-gce/pull/322) ([rramkumar1](https://github.com/rramkumar1))
- IAP + CDN e2e testing implementation [\#319](https://github.com/kubernetes/ingress-gce/pull/319) ([rramkumar1](https://github.com/rramkumar1))
- Break out some helper functions for better testing + reuse [\#318](https://github.com/kubernetes/ingress-gce/pull/318) ([rramkumar1](https://github.com/rramkumar1))
- Move cloud to ControllerContext [\#316](https://github.com/kubernetes/ingress-gce/pull/316) ([nicksardo](https://github.com/nicksardo))
- Support caching in echoserver [\#315](https://github.com/kubernetes/ingress-gce/pull/315) ([rramkumar1](https://github.com/rramkumar1))
- Add server version to echo [\#314](https://github.com/kubernetes/ingress-gce/pull/314) ([nicksardo](https://github.com/nicksardo))
- Moved BackendService composite type to its own package [\#313](https://github.com/kubernetes/ingress-gce/pull/313) ([rramkumar1](https://github.com/rramkumar1))
- Simple web server for testing ingress-gce features [\#312](https://github.com/kubernetes/ingress-gce/pull/312) ([nicksardo](https://github.com/nicksardo))
-  Add beta backend service support to composite type [\#310](https://github.com/kubernetes/ingress-gce/pull/310) ([MrHohn](https://github.com/MrHohn))
- Add version and gitcommit to the e2e test [\#309](https://github.com/kubernetes/ingress-gce/pull/309) ([bowei](https://github.com/bowei))
- Use cloud ResourceID for URL parsing, generation, and comparison [\#308](https://github.com/kubernetes/ingress-gce/pull/308) ([nicksardo](https://github.com/nicksardo))
- Revert "Use cloud ResourceID for URL parsing and generation" [\#307](https://github.com/kubernetes/ingress-gce/pull/307) ([nicksardo](https://github.com/nicksardo))
- Add skeleton for the e2e tests [\#306](https://github.com/kubernetes/ingress-gce/pull/306) ([bowei](https://github.com/bowei))
- Store feature names in backend service description [\#305](https://github.com/kubernetes/ingress-gce/pull/305) ([MrHohn](https://github.com/MrHohn))
- Use cloud ResourceID for URL parsing and generation [\#304](https://github.com/kubernetes/ingress-gce/pull/304) ([nicksardo](https://github.com/nicksardo))
- Use generated mocks to implement unit tests for pkg/backends  [\#303](https://github.com/kubernetes/ingress-gce/pull/303) ([rramkumar1](https://github.com/rramkumar1))
- Split l7.go into resource-specific files \(no logic changes\) [\#302](https://github.com/kubernetes/ingress-gce/pull/302) ([nicksardo](https://github.com/nicksardo))
- IAP + CDN  [\#301](https://github.com/kubernetes/ingress-gce/pull/301) ([rramkumar1](https://github.com/rramkumar1))
- Add IngressValidator and supporting utilities [\#300](https://github.com/kubernetes/ingress-gce/pull/300) ([bowei](https://github.com/bowei))
- Slight refactor of controller context to include both NEG & BackendConfig switches [\#299](https://github.com/kubernetes/ingress-gce/pull/299) ([rramkumar1](https://github.com/rramkumar1))
- Update vendor for gce provider [\#293](https://github.com/kubernetes/ingress-gce/pull/293) ([MrHohn](https://github.com/MrHohn))
- Add support for security policy [\#291](https://github.com/kubernetes/ingress-gce/pull/291) ([MrHohn](https://github.com/MrHohn))
- Add custom validation for BackendConfig + hook validation into Translator [\#289](https://github.com/kubernetes/ingress-gce/pull/289) ([rramkumar1](https://github.com/rramkumar1))
- Fix bug with ensuring BackendService settings + health checks [\#288](https://github.com/kubernetes/ingress-gce/pull/288) ([rramkumar1](https://github.com/rramkumar1))
- Add SECURITY\_CONTACTS file [\#287](https://github.com/kubernetes/ingress-gce/pull/287) ([nicksardo](https://github.com/nicksardo))
- Bake backend config into ServicePort [\#285](https://github.com/kubernetes/ingress-gce/pull/285) ([MrHohn](https://github.com/MrHohn))
- Add annotation for exposing NEGs [\#284](https://github.com/kubernetes/ingress-gce/pull/284) ([agau4779](https://github.com/agau4779))
- Make sure structs in OpenAPI spec are serialized with 'type: object' [\#283](https://github.com/kubernetes/ingress-gce/pull/283) ([rramkumar1](https://github.com/rramkumar1))
- Define security policy API in BackendConfig [\#281](https://github.com/kubernetes/ingress-gce/pull/281) ([MrHohn](https://github.com/MrHohn))
- Refactor pkg/backends to use new BackendService composite type  [\#280](https://github.com/kubernetes/ingress-gce/pull/280) ([rramkumar1](https://github.com/rramkumar1))
- Ensure Load Balancer using IG links instead of IG compute object [\#279](https://github.com/kubernetes/ingress-gce/pull/279) ([rramkumar1](https://github.com/rramkumar1))
- Update documentation to include multiple-TLS support. [\#278](https://github.com/kubernetes/ingress-gce/pull/278) ([rramkumar1](https://github.com/rramkumar1))
- Update gce provider in vendor [\#277](https://github.com/kubernetes/ingress-gce/pull/277) ([MrHohn](https://github.com/MrHohn))
- fake backendservices save alpha objects by default [\#276](https://github.com/kubernetes/ingress-gce/pull/276) ([agau4779](https://github.com/agau4779))
- TranslateIngress changes [\#275](https://github.com/kubernetes/ingress-gce/pull/275) ([nicksardo](https://github.com/nicksardo))
- Fix BackendConfigKey to use beta [\#273](https://github.com/kubernetes/ingress-gce/pull/273) ([MrHohn](https://github.com/MrHohn))
- Add simple validation for BackendConfig using OpenAPI schema generation  [\#272](https://github.com/kubernetes/ingress-gce/pull/272) ([rramkumar1](https://github.com/rramkumar1))
- BackendConfig v1alpha1-\>v1beta1 [\#271](https://github.com/kubernetes/ingress-gce/pull/271) ([MrHohn](https://github.com/MrHohn))
- Pass service/port tuple separate from ServicePort [\#270](https://github.com/kubernetes/ingress-gce/pull/270) ([nicksardo](https://github.com/nicksardo))
- Add event handlers for BackendConfig [\#268](https://github.com/kubernetes/ingress-gce/pull/268) ([rramkumar1](https://github.com/rramkumar1))
- Fix typo in faq [\#267](https://github.com/kubernetes/ingress-gce/pull/267) ([ChristianAlexander](https://github.com/ChristianAlexander))
- Small aesthetic fixes to code base. [\#266](https://github.com/kubernetes/ingress-gce/pull/266) ([rramkumar1](https://github.com/rramkumar1))
- Introduce configuration for IAP & CDN into BackendConfig spec [\#265](https://github.com/kubernetes/ingress-gce/pull/265) ([rramkumar1](https://github.com/rramkumar1))
- Condense health checkers into one health checker for all backends. [\#264](https://github.com/kubernetes/ingress-gce/pull/264) ([rramkumar1](https://github.com/rramkumar1))
- BackendService naming for NEG backend services & healthchecks [\#262](https://github.com/kubernetes/ingress-gce/pull/262) ([nicksardo](https://github.com/nicksardo))
- Update healthcheck docs regarding containerPort [\#260](https://github.com/kubernetes/ingress-gce/pull/260) ([sonu27](https://github.com/sonu27))
- Use leader election to prevent multiple controllers running [\#258](https://github.com/kubernetes/ingress-gce/pull/258) ([nicksardo](https://github.com/nicksardo))
-  Merge the logic of ToUrlMap\(\) and IngressToNodePorts\(\)  [\#257](https://github.com/kubernetes/ingress-gce/pull/257) ([rramkumar1](https://github.com/rramkumar1))
- Minor cleanup to docs and examples [\#256](https://github.com/kubernetes/ingress-gce/pull/256) ([nicksardo](https://github.com/nicksardo))
- Refactor gceurlmap to a struct representation [\#254](https://github.com/kubernetes/ingress-gce/pull/254) ([rramkumar1](https://github.com/rramkumar1))
- Add utils for retrieving backendconfigs for service \(and reversely\) [\#252](https://github.com/kubernetes/ingress-gce/pull/252) ([MrHohn](https://github.com/MrHohn))
- Re-vendor K8s to ~1.11.0-alpha.2 [\#248](https://github.com/kubernetes/ingress-gce/pull/248) ([nicksardo](https://github.com/nicksardo))
- Add util functions for backendConfig annotation [\#247](https://github.com/kubernetes/ingress-gce/pull/247) ([MrHohn](https://github.com/MrHohn))
- Condense backendPool and defaultBackendPool  [\#242](https://github.com/kubernetes/ingress-gce/pull/242) ([rramkumar1](https://github.com/rramkumar1))
- Rename serviceextension -\> backendconfig [\#240](https://github.com/kubernetes/ingress-gce/pull/240) ([MrHohn](https://github.com/MrHohn))
- Final changes to make MCI controller work  [\#238](https://github.com/kubernetes/ingress-gce/pull/238) ([rramkumar1](https://github.com/rramkumar1))
- Add an interface to manage target resources [\#237](https://github.com/kubernetes/ingress-gce/pull/237) ([rramkumar1](https://github.com/rramkumar1))
- Add code for building cluster client and other resources. [\#236](https://github.com/kubernetes/ingress-gce/pull/236) ([rramkumar1](https://github.com/rramkumar1))
- Small addition to ClusterServiceMapper interface [\#235](https://github.com/kubernetes/ingress-gce/pull/235) ([rramkumar1](https://github.com/rramkumar1))
- Add ability for MCI controller to enqueue ingresses [\#234](https://github.com/kubernetes/ingress-gce/pull/234) ([rramkumar1](https://github.com/rramkumar1))
- First pass at an interface to manage informers [\#233](https://github.com/kubernetes/ingress-gce/pull/233) ([rramkumar1](https://github.com/rramkumar1))
- Add support for logging latest commit hash of GLBC build being used [\#232](https://github.com/kubernetes/ingress-gce/pull/232) ([rramkumar1](https://github.com/rramkumar1))

## [v1.1.2](https://github.com/kubernetes/ingress-gce/tree/v1.1.2) (2018-04-18)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.1.1...v1.1.2)

**Merged pull requests:**

- Add support for logging latest commit hash of GLBC build being used [\#231](https://github.com/kubernetes/ingress-gce/pull/231) ([rramkumar1](https://github.com/rramkumar1))
- Add support for logging latest commit hash of GLBC build being used [\#230](https://github.com/kubernetes/ingress-gce/pull/230) ([rramkumar1](https://github.com/rramkumar1))
- Bump glbc.yaml to 1.1.1 [\#228](https://github.com/kubernetes/ingress-gce/pull/228) ([nicksardo](https://github.com/nicksardo))
- Add support for logging latest commit hash of GLBC build being used [\#226](https://github.com/kubernetes/ingress-gce/pull/226) ([rramkumar1](https://github.com/rramkumar1))
- Remove dead code for e2e testing. We do all e2e testing through k/k now [\#225](https://github.com/kubernetes/ingress-gce/pull/225) ([rramkumar1](https://github.com/rramkumar1))
- Update changelog for 1.1.1 [\#224](https://github.com/kubernetes/ingress-gce/pull/224) ([nicksardo](https://github.com/nicksardo))
- Integrate ClusterServiceMapper into translator.  [\#223](https://github.com/kubernetes/ingress-gce/pull/223) ([rramkumar1](https://github.com/rramkumar1))
- Update annotations.md [\#221](https://github.com/kubernetes/ingress-gce/pull/221) ([buzzedword](https://github.com/buzzedword))

## [v1.1.1](https://github.com/kubernetes/ingress-gce/tree/v1.1.1) (2018-04-16)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.1.0...v1.1.1)

**Merged pull requests:**

- Set glog levels in loadbalancer pool & fix markdown [\#218](https://github.com/kubernetes/ingress-gce/pull/218) ([nicksardo](https://github.com/nicksardo))
- Bootstrap multi-cluster controller [\#217](https://github.com/kubernetes/ingress-gce/pull/217) ([nicksardo](https://github.com/nicksardo))
- Cherrypick: Fix multiple secrets with same certificate [\#216](https://github.com/kubernetes/ingress-gce/pull/216) ([nicksardo](https://github.com/nicksardo))
- Code to setup removal of ServicePort logic out of translator  [\#214](https://github.com/kubernetes/ingress-gce/pull/214) ([rramkumar1](https://github.com/rramkumar1))
- Fix multiple secrets with same certificate [\#213](https://github.com/kubernetes/ingress-gce/pull/213) ([nicksardo](https://github.com/nicksardo))
- Add multi-cluster flag [\#212](https://github.com/kubernetes/ingress-gce/pull/212) ([nicksardo](https://github.com/nicksardo))
- Split sync up  [\#211](https://github.com/kubernetes/ingress-gce/pull/211) ([nicksardo](https://github.com/nicksardo))
- Update post-release-steps.md [\#210](https://github.com/kubernetes/ingress-gce/pull/210) ([rramkumar1](https://github.com/rramkumar1))
- Refactor translator.ToURLMap to not re-fetch backend services [\#207](https://github.com/kubernetes/ingress-gce/pull/207) ([rramkumar1](https://github.com/rramkumar1))
- Introduce MultiClusterContext as part of ControllerContext [\#203](https://github.com/kubernetes/ingress-gce/pull/203) ([rramkumar1](https://github.com/rramkumar1))
- Vendor in Cluster Registry code [\#202](https://github.com/kubernetes/ingress-gce/pull/202) ([rramkumar1](https://github.com/rramkumar1))
- Add ServiceExtension CRD lifecycle management and empty spec definition [\#163](https://github.com/kubernetes/ingress-gce/pull/163) ([MrHohn](https://github.com/MrHohn))

## [v1.1.0](https://github.com/kubernetes/ingress-gce/tree/v1.1.0) (2018-04-11)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.0.1...v1.1.0)

**Closed issues:**

- Some links in 'ingress' repo GCE release notes are 404 [\#30](https://github.com/kubernetes/ingress-gce/issues/30)

**Merged pull requests:**

- Remove duplicate nodeport translation [\#206](https://github.com/kubernetes/ingress-gce/pull/206) ([nicksardo](https://github.com/nicksardo))
- Start changelog file [\#205](https://github.com/kubernetes/ingress-gce/pull/205) ([nicksardo](https://github.com/nicksardo))
- Change naming of SSL certs [\#200](https://github.com/kubernetes/ingress-gce/pull/200) ([nicksardo](https://github.com/nicksardo))
- Ensure only needed nodeports on instance group on ingress sync [\#199](https://github.com/kubernetes/ingress-gce/pull/199) ([MrHohn](https://github.com/MrHohn))
- Ensure .go/cache in build [\#198](https://github.com/kubernetes/ingress-gce/pull/198) ([MrHohn](https://github.com/MrHohn))
- Support for multiple TLS certificates [\#195](https://github.com/kubernetes/ingress-gce/pull/195) ([rramkumar1](https://github.com/rramkumar1))
- Update vendor/ to support multiple TLS certificates interface [\#193](https://github.com/kubernetes/ingress-gce/pull/193) ([bowei](https://github.com/bowei))
- Bump golang build image to 1.10 [\#192](https://github.com/kubernetes/ingress-gce/pull/192) ([nicksardo](https://github.com/nicksardo))
- Small changes to deploy/glbc/README [\#191](https://github.com/kubernetes/ingress-gce/pull/191) ([rramkumar1](https://github.com/rramkumar1))
- Update glbc manifest to v1.0.1 [\#190](https://github.com/kubernetes/ingress-gce/pull/190) ([nicksardo](https://github.com/nicksardo))
- Satisfy golang 1.10 vetting [\#189](https://github.com/kubernetes/ingress-gce/pull/189) ([nicksardo](https://github.com/nicksardo))
- Add some documentation on post-release TODO's [\#188](https://github.com/kubernetes/ingress-gce/pull/188) ([rramkumar1](https://github.com/rramkumar1))
- Add Ingress HTTP2 feature gate [\#161](https://github.com/kubernetes/ingress-gce/pull/161) ([agau4779](https://github.com/agau4779))

## [v1.0.1](https://github.com/kubernetes/ingress-gce/tree/v1.0.1) (2018-04-03)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.0.0...v1.0.1)

**Closed issues:**

- Test Failing: \[sig-network\] Loadbalancing: L7 GCE \[Slow\] \[Feature:Ingress\] multicluster ingress should get instance group annotation [\#185](https://github.com/kubernetes/ingress-gce/issues/185)
- ingress controller should only manage instance groups for multicluster ingress [\#182](https://github.com/kubernetes/ingress-gce/issues/182)
- Missing ListUrlMaps from LoadBalancers interface [\#162](https://github.com/kubernetes/ingress-gce/issues/162)
- `dep ensure` is broken [\#155](https://github.com/kubernetes/ingress-gce/issues/155)
- Duplicate patch from PR \#148 into main repo. [\#154](https://github.com/kubernetes/ingress-gce/issues/154)
- ingress-gce major refactor and performance fixes [\#152](https://github.com/kubernetes/ingress-gce/issues/152)

**Merged pull requests:**

- Cherry-pick checkpoint changes to 1.0 [\#187](https://github.com/kubernetes/ingress-gce/pull/187) ([nicksardo](https://github.com/nicksardo))
- Fix sync of multi-cluster ingress [\#183](https://github.com/kubernetes/ingress-gce/pull/183) ([nicksardo](https://github.com/nicksardo))
- Fix path default-http-backend.yaml [\#180](https://github.com/kubernetes/ingress-gce/pull/180) ([atotto](https://github.com/atotto))
- Updating FakeLoadBalancers.Delete to return NotFound when appropriate [\#178](https://github.com/kubernetes/ingress-gce/pull/178) ([nikhiljindal](https://github.com/nikhiljindal))
- Minor spelling and capitalization changes [\#177](https://github.com/kubernetes/ingress-gce/pull/177) ([nicksardo](https://github.com/nicksardo))
- Checkpoint\(\) takes a single LB rather than a list of LBs [\#170](https://github.com/kubernetes/ingress-gce/pull/170) ([bowei](https://github.com/bowei))
- Use given name rather than regenerating it in UrlMap fake [\#169](https://github.com/kubernetes/ingress-gce/pull/169) ([nikhiljindal](https://github.com/nikhiljindal))
- Update testing.md [\#168](https://github.com/kubernetes/ingress-gce/pull/168) ([AdamDang](https://github.com/AdamDang))
- Adding ListUrlMaps to LoadBalancers interface [\#165](https://github.com/kubernetes/ingress-gce/pull/165) ([nikhiljindal](https://github.com/nikhiljindal))
- Fix formatting error in docs/README.md [\#153](https://github.com/kubernetes/ingress-gce/pull/153) ([rramkumar1](https://github.com/rramkumar1))
- Ingress HTTP/2 Support [\#146](https://github.com/kubernetes/ingress-gce/pull/146) ([agau4779](https://github.com/agau4779))

## [v1.0.0](https://github.com/kubernetes/ingress-gce/tree/v1.0.0) (2018-03-16)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v0.9.8-alpha.2...v1.0.0)

**Closed issues:**

- sitemap endpoint? [\#145](https://github.com/kubernetes/ingress-gce/issues/145)
- "internal" ingresses [\#138](https://github.com/kubernetes/ingress-gce/issues/138)
- Multiple Healthcheck Requests from GCP L7 [\#137](https://github.com/kubernetes/ingress-gce/issues/137)
- Issue closed without comment [\#134](https://github.com/kubernetes/ingress-gce/issues/134)
- Document ingress.gcp.kubernetes.io/pre-shared-cert annotation [\#52](https://github.com/kubernetes/ingress-gce/issues/52)
- GCE: WebSocket: connection is broken CloseEvent {isTrusted: true, wasClean: false, code: 1006, reason: "", type: "close", …} [\#36](https://github.com/kubernetes/ingress-gce/issues/36)
- GCE: respect static-ip assignment via update  [\#26](https://github.com/kubernetes/ingress-gce/issues/26)
- GCE cloud: add a kube-specific header to GCE API calls [\#22](https://github.com/kubernetes/ingress-gce/issues/22)
- e2e test leaves garbage around [\#21](https://github.com/kubernetes/ingress-gce/issues/21)
- GLBC ingress: only handle annotated ingress [\#20](https://github.com/kubernetes/ingress-gce/issues/20)
- Point gce ingress health checks at the node for onlylocal services [\#17](https://github.com/kubernetes/ingress-gce/issues/17)

**Merged pull requests:**

- Fix copyright in deploy/glbc/script.sh [\#151](https://github.com/kubernetes/ingress-gce/pull/151) ([rramkumar1](https://github.com/rramkumar1))
- Cleanup some unused files [\#150](https://github.com/kubernetes/ingress-gce/pull/150) ([rramkumar1](https://github.com/rramkumar1))
- Initial implementation for ingress rate limiting [\#148](https://github.com/kubernetes/ingress-gce/pull/148) ([rramkumar1](https://github.com/rramkumar1))
- update: s\_k/ingress-k/ingress-gce\_ in annotations.md [\#147](https://github.com/kubernetes/ingress-gce/pull/147) ([G-Harmon](https://github.com/G-Harmon))
- Adding information about using GCP SSL certs for frontend HTTPS [\#144](https://github.com/kubernetes/ingress-gce/pull/144) ([nikhiljindal](https://github.com/nikhiljindal))
- Add instructions and a tool for people who want to try out a new version of the ingress controller before it is released. [\#140](https://github.com/kubernetes/ingress-gce/pull/140) ([rramkumar1](https://github.com/rramkumar1))
- fix event message for attach/detach NEs [\#139](https://github.com/kubernetes/ingress-gce/pull/139) ([freehan](https://github.com/freehan))

## [v0.9.8-alpha.2](https://github.com/kubernetes/ingress-gce/tree/v0.9.8-alpha.2) (2018-02-12)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v0.9.8-alpha.1...v0.9.8-alpha.2)

**Closed issues:**

- \[GLBC\] Surface event when front-end not created [\#41](https://github.com/kubernetes/ingress-gce/issues/41)

**Merged pull requests:**

- Verbose flag: set v=3 [\#135](https://github.com/kubernetes/ingress-gce/pull/135) ([nicksardo](https://github.com/nicksardo))

## [v0.9.8-alpha.1](https://github.com/kubernetes/ingress-gce/tree/v0.9.8-alpha.1) (2018-02-09)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v0.9.7...v0.9.8-alpha.1)

**Closed issues:**

- Ingress name has random trailing hash in events [\#130](https://github.com/kubernetes/ingress-gce/issues/130)
- Support for rewrite-target annotation [\#124](https://github.com/kubernetes/ingress-gce/issues/124)
- Ingress without default backend explicitly configured doesn't work at all [\#110](https://github.com/kubernetes/ingress-gce/issues/110)
- Wrong health check with end-to-end https scheme [\#105](https://github.com/kubernetes/ingress-gce/issues/105)
- Large file upload fails after 30 seconds [\#102](https://github.com/kubernetes/ingress-gce/issues/102)
- Ingress E2E setup is breaking  [\#93](https://github.com/kubernetes/ingress-gce/issues/93)
- examples/websocket/server.go: concurrent write to socket [\#77](https://github.com/kubernetes/ingress-gce/issues/77)
- Invalid value for field 'namedPorts\[\*\].port': '0' [\#75](https://github.com/kubernetes/ingress-gce/issues/75)
- support for proper health checks [\#65](https://github.com/kubernetes/ingress-gce/issues/65)
- support for session affinity [\#64](https://github.com/kubernetes/ingress-gce/issues/64)
- TLS certificate validations causes tls creation to fail [\#61](https://github.com/kubernetes/ingress-gce/issues/61)
- GLBC: Ingress can't be properly created: Insufficient Permission [\#49](https://github.com/kubernetes/ingress-gce/issues/49)
- GLBC: Ingress can't be properly created: Insufficient Permission [\#47](https://github.com/kubernetes/ingress-gce/issues/47)
- GLBC: Each ingress sync updates resources for all ingresses [\#44](https://github.com/kubernetes/ingress-gce/issues/44)
- Disabled HttpLoadBalancing, unable to create Ingress with glbc:0.9.1 [\#29](https://github.com/kubernetes/ingress-gce/issues/29)
- \[GLBC\] Expose GCE backend parameters in Ingress object API [\#27](https://github.com/kubernetes/ingress-gce/issues/27)
- High glbc CPU usage [\#25](https://github.com/kubernetes/ingress-gce/issues/25)
- Specify IP addresses the Ingress controller is listening on [\#24](https://github.com/kubernetes/ingress-gce/issues/24)
- Add e2e testing [\#16](https://github.com/kubernetes/ingress-gce/issues/16)

**Merged pull requests:**

- Emit event on RuntimeInfo error [\#133](https://github.com/kubernetes/ingress-gce/pull/133) ([MrHohn](https://github.com/MrHohn))
- Periodic resync [\#129](https://github.com/kubernetes/ingress-gce/pull/129) ([nicksardo](https://github.com/nicksardo))
- Rename Port to NodePort [\#128](https://github.com/kubernetes/ingress-gce/pull/128) ([nicksardo](https://github.com/nicksardo))
- Add some documentation for how to run e2e tests locally. [\#126](https://github.com/kubernetes/ingress-gce/pull/126) ([rramkumar1](https://github.com/rramkumar1))
- sync node on node status change [\#125](https://github.com/kubernetes/ingress-gce/pull/125) ([freehan](https://github.com/freehan))
- Sync ingress-specific backends and minor logging changes [\#123](https://github.com/kubernetes/ingress-gce/pull/123) ([nicksardo](https://github.com/nicksardo))
- Firewall Sync: Allow entire nodeport range [\#122](https://github.com/kubernetes/ingress-gce/pull/122) ([nicksardo](https://github.com/nicksardo))
- Introduce flag endpoint [\#121](https://github.com/kubernetes/ingress-gce/pull/121) ([nicksardo](https://github.com/nicksardo))
- fix typo [\#120](https://github.com/kubernetes/ingress-gce/pull/120) ([bowei](https://github.com/bowei))
- Always set -logtostderr \(this matches the original behavior\) [\#119](https://github.com/kubernetes/ingress-gce/pull/119) ([bowei](https://github.com/bowei))
- Code review comments [\#117](https://github.com/kubernetes/ingress-gce/pull/117) ([bowei](https://github.com/bowei))
- Remove some vars from make push-e2e target [\#115](https://github.com/kubernetes/ingress-gce/pull/115) ([rramkumar1](https://github.com/rramkumar1))
- Update README.md to point to example [\#114](https://github.com/kubernetes/ingress-gce/pull/114) ([zrosenbauer](https://github.com/zrosenbauer))
- Update vendor [\#113](https://github.com/kubernetes/ingress-gce/pull/113) ([bowei](https://github.com/bowei))
- Emit event on TLS errors [\#112](https://github.com/kubernetes/ingress-gce/pull/112) ([MrHohn](https://github.com/MrHohn))
- Add unit test to verify pre-shared cert retention [\#111](https://github.com/kubernetes/ingress-gce/pull/111) ([MrHohn](https://github.com/MrHohn))
- Remove .travis.yml [\#108](https://github.com/kubernetes/ingress-gce/pull/108) ([rramkumar1](https://github.com/rramkumar1))
- Add unit test for the generated GCE config reader func [\#107](https://github.com/kubernetes/ingress-gce/pull/107) ([MrHohn](https://github.com/MrHohn))
- Sync one ingress [\#106](https://github.com/kubernetes/ingress-gce/pull/106) ([bowei](https://github.com/bowei))
- Remove the duplicate health check example and restructure example folder [\#104](https://github.com/kubernetes/ingress-gce/pull/104) ([MrHohn](https://github.com/MrHohn))
- Modify VERSION to use "latest" in push-e2e target [\#103](https://github.com/kubernetes/ingress-gce/pull/103) ([rramkumar1](https://github.com/rramkumar1))
- Cleanup [\#101](https://github.com/kubernetes/ingress-gce/pull/101) ([bowei](https://github.com/bowei))
- Deprecate --use-real-cloud and --verbose flags [\#100](https://github.com/kubernetes/ingress-gce/pull/100) ([bowei](https://github.com/bowei))
- Fix push-e2e make rule [\#99](https://github.com/kubernetes/ingress-gce/pull/99) ([rramkumar1](https://github.com/rramkumar1))
- Fix gce [\#98](https://github.com/kubernetes/ingress-gce/pull/98) ([bowei](https://github.com/bowei))
- Cleanup [\#97](https://github.com/kubernetes/ingress-gce/pull/97) ([bowei](https://github.com/bowei))
- Update vendor [\#96](https://github.com/kubernetes/ingress-gce/pull/96) ([bowei](https://github.com/bowei))
- Minor cleanup [\#92](https://github.com/kubernetes/ingress-gce/pull/92) ([bowei](https://github.com/bowei))
- Translator [\#91](https://github.com/kubernetes/ingress-gce/pull/91) ([bowei](https://github.com/bowei))
- Move taskQueue to utils.PeriodicTaskQueue [\#90](https://github.com/kubernetes/ingress-gce/pull/90) ([bowei](https://github.com/bowei))
- Delete unreferenced constants [\#89](https://github.com/kubernetes/ingress-gce/pull/89) ([bowei](https://github.com/bowei))
- Add e2e make rule [\#88](https://github.com/kubernetes/ingress-gce/pull/88) ([rramkumar1](https://github.com/rramkumar1))
- Add code-of-conduct.md [\#86](https://github.com/kubernetes/ingress-gce/pull/86) ([spiffxp](https://github.com/spiffxp))
- Minor fixes to example JS [\#85](https://github.com/kubernetes/ingress-gce/pull/85) ([nicksardo](https://github.com/nicksardo))
- Fixing typos in gce-tls example readme [\#84](https://github.com/kubernetes/ingress-gce/pull/84) ([nikhiljindal](https://github.com/nikhiljindal))
- Add build, coverage and report badges [\#82](https://github.com/kubernetes/ingress-gce/pull/82) ([MrHohn](https://github.com/MrHohn))
- Update websocket example [\#80](https://github.com/kubernetes/ingress-gce/pull/80) ([nicksardo](https://github.com/nicksardo))
- Add ListGlobalForwardingRules to the LoadBalancers interface. [\#78](https://github.com/kubernetes/ingress-gce/pull/78) ([G-Harmon](https://github.com/G-Harmon))
- Add ability to change prefix in the Namer [\#74](https://github.com/kubernetes/ingress-gce/pull/74) ([bowei](https://github.com/bowei))
- Add unit test for functions in namer [\#73](https://github.com/kubernetes/ingress-gce/pull/73) ([bowei](https://github.com/bowei))
- Move GetNamedPort to Namer [\#72](https://github.com/kubernetes/ingress-gce/pull/72) ([bowei](https://github.com/bowei))
- Centralize more of the naming of GCE resources [\#70](https://github.com/kubernetes/ingress-gce/pull/70) ([bowei](https://github.com/bowei))
- Removing non code test's dependency on testing package [\#69](https://github.com/kubernetes/ingress-gce/pull/69) ([nikhiljindal](https://github.com/nikhiljindal))
- Split namer into its own file [\#68](https://github.com/kubernetes/ingress-gce/pull/68) ([bowei](https://github.com/bowei))
- Rename OWNERS assignees: to approvers: [\#66](https://github.com/kubernetes/ingress-gce/pull/66) ([spiffxp](https://github.com/spiffxp))
- move neg annotation to annotations package [\#62](https://github.com/kubernetes/ingress-gce/pull/62) ([freehan](https://github.com/freehan))
- Extracting tlsloader into a separate package to enable reuse [\#59](https://github.com/kubernetes/ingress-gce/pull/59) ([nikhiljindal](https://github.com/nikhiljindal))
- Extracting out annotations to a separate package to allow reuse [\#58](https://github.com/kubernetes/ingress-gce/pull/58) ([nikhiljindal](https://github.com/nikhiljindal))
- Updating loadbalancer/fakes.go to return 404 [\#57](https://github.com/kubernetes/ingress-gce/pull/57) ([nikhiljindal](https://github.com/nikhiljindal))
- Updating backends/fakes to return 404 in the same way as all other fakes [\#56](https://github.com/kubernetes/ingress-gce/pull/56) ([nikhiljindal](https://github.com/nikhiljindal))
- revendor k8s cloud provider and its dependencies [\#54](https://github.com/kubernetes/ingress-gce/pull/54) ([freehan](https://github.com/freehan))
- Rename local var to reflect what it is. [\#53](https://github.com/kubernetes/ingress-gce/pull/53) ([G-Harmon](https://github.com/G-Harmon))
- K8s-NEG Integration [\#48](https://github.com/kubernetes/ingress-gce/pull/48) ([freehan](https://github.com/freehan))
- Update build [\#15](https://github.com/kubernetes/ingress-gce/pull/15) ([bowei](https://github.com/bowei))
- Update travis [\#12](https://github.com/kubernetes/ingress-gce/pull/12) ([bowei](https://github.com/bowei))
- PRE-NEG changes [\#11](https://github.com/kubernetes/ingress-gce/pull/11) ([freehan](https://github.com/freehan))

## [v0.9.7](https://github.com/kubernetes/ingress-gce/tree/v0.9.7) (2017-10-10)
**Merged pull requests:**

- Fix the glbc build by removing 'godeps' from command. [\#10](https://github.com/kubernetes/ingress-gce/pull/10) ([G-Harmon](https://github.com/G-Harmon))
- Get value of string pointer for log message [\#9](https://github.com/kubernetes/ingress-gce/pull/9) ([nicksardo](https://github.com/nicksardo))
- Release 0.9.7 [\#8](https://github.com/kubernetes/ingress-gce/pull/8) ([nicksardo](https://github.com/nicksardo))
- Stop ignoring test files and non-go files in vendor [\#7](https://github.com/kubernetes/ingress-gce/pull/7) ([nicksardo](https://github.com/nicksardo))
- Minor cleanup to instance group management [\#6](https://github.com/kubernetes/ingress-gce/pull/6) ([nicksardo](https://github.com/nicksardo))
- Move main.go to cmd/glbc [\#5](https://github.com/kubernetes/ingress-gce/pull/5) ([nicksardo](https://github.com/nicksardo))
- Migrate to dep [\#4](https://github.com/kubernetes/ingress-gce/pull/4) ([nicksardo](https://github.com/nicksardo))
- Fix issue when setting instance group named ports [\#3](https://github.com/kubernetes/ingress-gce/pull/3) ([nicksardo](https://github.com/nicksardo))
- Update repo to be GCE specific [\#2](https://github.com/kubernetes/ingress-gce/pull/2) ([nicksardo](https://github.com/nicksardo))
- Handle forbiddenError for XPN clusters by raising event [\#1](https://github.com/kubernetes/ingress-gce/pull/1) ([nicksardo](https://github.com/nicksardo))

\* *This Changelog was automatically generated by [github_changelog_generator](https://github.com/github-changelog-generator/github-changelog-generator)*


\* *This Changelog was automatically generated by [github_changelog_generator](https://github.com/github-changelog-generator/github-changelog-generator)*


\* *This Change Log was automatically generated by [github_changelog_generator](https://github.com/skywinder/Github-Changelog-Generator)*
