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
- deploy csm neg scirpt and yaml [\#882](https://github.com/kubernetes/ingress-gce/pull/882) ([cadmuxe](https://github.com/cadmuxe))
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

## [v1.7.0](https://github.com/kubernetes/ingress-gce/tree/v1.7.0) (2019-09-26)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.6.0...v1.7.0)

**Fixed bugs:**

- NEG controller should create NEG for default backend when enabled [\#767](https://github.com/kubernetes/ingress-gce/issues/767)
- Removing Node Pool from Cluster Breaks Ingress Conroller [\#649](https://github.com/kubernetes/ingress-gce/issues/649)
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

## [v1.6.0](https://github.com/kubernetes/ingress-gce/tree/v1.6.0) (2019-06-14)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.5.2...v1.6.0)

**Implemented enhancements:**

- Unclear documentation of spec.rules.http.paths [\#181](https://github.com/kubernetes/ingress-gce/issues/181)
- Add example using grpc and http2  [\#18](https://github.com/kubernetes/ingress-gce/issues/18)

**Closed issues:**

- HTTS frontend listener isn't deleted after setting ingress.allow-http: "false" annotation [\#766](https://github.com/kubernetes/ingress-gce/issues/766)
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
- Adding the /healthz handler to the 404-default-server-with-metris to … [\#747](https://github.com/kubernetes/ingress-gce/pull/747) ([vbannai](https://github.com/vbannai))
- Update canonical rbac.yaml with latest, minimal bindings [\#746](https://github.com/kubernetes/ingress-gce/pull/746) ([dekkagaijin](https://github.com/dekkagaijin))
- Adding docker configuration file for the 404-server-with-metrics and … [\#745](https://github.com/kubernetes/ingress-gce/pull/745) ([vbannai](https://github.com/vbannai))
- More composite types [\#742](https://github.com/kubernetes/ingress-gce/pull/742) ([spencerhance](https://github.com/spencerhance))
- Switch to go modules [\#735](https://github.com/kubernetes/ingress-gce/pull/735) ([spencerhance](https://github.com/spencerhance))

## [v1.5.2](https://github.com/kubernetes/ingress-gce/tree/v1.5.2) (2019-05-01)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.5.1...v1.5.2)

**Implemented enhancements:**

- Link for the example on deploying ingress controller is not valid [\#686](https://github.com/kubernetes/ingress-gce/issues/686)
- If readiness probe is on port different than the service \(app\) port - ingress failes to sync the correct healthcheck [\#647](https://github.com/kubernetes/ingress-gce/issues/647)

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
- ErrImagePull: k8s.gcr.io/defaultbackend:1.5 not found [\#549](https://github.com/kubernetes/ingress-gce/issues/549)
- configure maxRatePerInstance in backend [\#545](https://github.com/kubernetes/ingress-gce/issues/545)
- GKE BackendConfig permissions change `container.backendConfigs.get` does not exist [\#538](https://github.com/kubernetes/ingress-gce/issues/538)
- A new home for 404-server \(defaultbackend\) [\#498](https://github.com/kubernetes/ingress-gce/issues/498)
- Does not work if workers are in different subnet.  [\#282](https://github.com/kubernetes/ingress-gce/issues/282)
- original http request origin and host headers are overriden [\#179](https://github.com/kubernetes/ingress-gce/issues/179)

**Merged pull requests:**

- Modify NameBelongToCluter to tolerate truncated cluster name suffix [\#650](https://github.com/kubernetes/ingress-gce/pull/650) ([freehan](https://github.com/freehan))
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
- do not use condition predicate while getting zone for node [\#576](https://github.com/kubernetes/ingress-gce/pull/576) ([agadelshin](https://github.com/agadelshin))
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
- do not leak LB when ingress class is changed [\#547](https://github.com/kubernetes/ingress-gce/pull/547) ([agadelshin](https://github.com/agadelshin))
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
- Add a Backofff Handler utils [\#499](https://github.com/kubernetes/ingress-gce/pull/499) ([freehan](https://github.com/freehan))

# Change Log

## [v1.4.3](https://github.com/kubernetes/ingress-gce/tree/v1.4.3) (2019-02-12)
[Full Changelog](https://github.com/kubernetes/ingress-gce/compare/v1.4.2...v1.4.3)

**Closed issues:**

- GCP - Kubernetes Ingress Backend service unhealthy [\#621](https://github.com/kubernetes/ingress-gce/issues/621)
- BackendConfig security policy not enforced  [\#616](https://github.com/kubernetes/ingress-gce/issues/616)
- original http request origin and host headers are overriden [\#179](https://github.com/kubernetes/ingress-gce/issues/179)

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
- ErrImagePull: k8s.gcr.io/defaultbackend:1.5 not found [\#549](https://github.com/kubernetes/ingress-gce/issues/549)
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

- GCE ingress stucks on "Creating ingress" status, existing ingresses don't update [\#470](https://github.com/kubernetes/ingress-gce/issues/470)
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
- Typo in message: SyncNetworkEndpiontGroupFailed-\>SyncNetworkEndpointGroupFailed [\#374](https://github.com/kubernetes/ingress-gce/pull/374) ([AdamDang](https://github.com/AdamDang))
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
- Swtich to use beta HealthCheck for NEG [\#366](https://github.com/kubernetes/ingress-gce/pull/366) ([freehan](https://github.com/freehan))
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
- On removal of backend config name from service annotaion, ensure no existing settings are affected [\#347](https://github.com/kubernetes/ingress-gce/pull/347) ([rramkumar1](https://github.com/rramkumar1))
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

\* *This Change Log was automatically generated by [github_changelog_generator](https://github.com/skywinder/Github-Changelog-Generator)*
