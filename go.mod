module github.com/coder/envbox

go 1.20

// Coder runs a fork of tailscale, since we depend on that repo we need to make
// sure go.mod doesn't try to use the actual tailscale libs otherwise things
// won't compile.
replace tailscale.com => github.com/coder/tailscale v1.1.1-0.20230314023417-d9efcc0ac972

require (
	cdr.dev/slog v1.4.2-0.20230228204227-60d22dceaf04
	github.com/coder/coder v0.20.2-0.20230323233815-982274536509
	github.com/coder/retry v1.3.1-0.20230210155434-e90a2e1e091d
	github.com/docker/docker v23.0.1+incompatible
	github.com/opencontainers/image-spec v1.1.0-rc2
	github.com/ory/dockertest/v3 v3.9.1
	github.com/quasilyte/go-ruleguard/dsl v0.3.22
	github.com/spf13/afero v1.9.4
	github.com/spf13/cobra v1.6.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.8.2
	github.com/vishvananda/netlink v1.1.1-0.20211118161826-650dca95af54
	golang.org/x/exp v0.0.0-20230321023759-10a507213a29
	golang.org/x/net v0.8.0
	golang.org/x/sys v0.6.0
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2
	k8s.io/mount-utils v0.26.2
	k8s.io/utils v0.0.0-20230220204549-a5ecb0141aa5
)

require (
	cloud.google.com/go/compute v1.18.0 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	filippo.io/edwards25519 v1.0.0-rc.1 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20230124172434-306776ec8161 // indirect
	github.com/Microsoft/go-winio v0.6.0 // indirect
	github.com/Nvveen/Gotty v0.0.0-20120604004816-cd527374f1e5 // indirect
	github.com/OneOfOne/xxhash v1.2.8 // indirect
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/agnivade/levenshtein v1.1.1 // indirect
	github.com/akutz/memconn v0.1.0 // indirect
	github.com/alecthomas/chroma v0.10.0 // indirect
	github.com/alexbrainman/sspi v0.0.0-20210105120005-909beea2cc74 // indirect
	github.com/apparentlymart/go-textseg/v13 v13.0.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.2.0 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/coder/terraform-provider-coder v0.6.21 // indirect
	github.com/containerd/continuity v0.3.0 // indirect
	github.com/coreos/go-iptables v0.6.0 // indirect
	github.com/coreos/go-oidc/v3 v3.4.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dlclark/regexp2 v1.8.1 // indirect
	github.com/docker/cli v23.0.1+incompatible // indirect
	github.com/docker/distribution v2.8.1+incompatible // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/fatih/color v1.14.1 // indirect
	github.com/fxamacker/cbor/v2 v2.4.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-chi/chi/v5 v5.0.7 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/glog v1.0.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.15.1 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cty v1.4.1-0.20200414143053-d3edf31b6320 // indirect
	github.com/hashicorp/go-hclog v1.2.1 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/go-version v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.1 // indirect
	github.com/hashicorp/hcl/v2 v2.14.0 // indirect
	github.com/hashicorp/logutils v1.0.0 // indirect
	github.com/hashicorp/terraform-plugin-go v0.12.0 // indirect
	github.com/hashicorp/terraform-plugin-log v0.7.0 // indirect
	github.com/hashicorp/terraform-plugin-sdk/v2 v2.20.0 // indirect
	github.com/hashicorp/yamux v0.0.0-20220718163420-dd80a7ee44ce // indirect
	github.com/hdevalence/ed25519consensus v0.0.0-20220222234857-c00d1f31bab3 // indirect
	github.com/iancoleman/strcase v0.2.0 // indirect
	github.com/illarion/gonotify v1.0.1 // indirect
	github.com/imdario/mergo v0.3.13 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/insomniacslk/dhcp v0.0.0-20221215072855-de60144f33f8 // indirect
	github.com/jmoiron/sqlx v1.3.5 // indirect
	github.com/josharian/native v1.1.1-0.20230202152459-5c7d0dd6ab86 // indirect
	github.com/jsimonetti/rtnetlink v1.1.2-0.20220408201609-d380b505068b // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.15.15 // indirect
	github.com/kortschak/wol v0.0.0-20200729010619-da482cc4850a // indirect
	github.com/lib/pq v1.10.6 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.17 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/mdlayher/genetlink v1.2.0 // indirect
	github.com/mdlayher/netlink v1.7.1 // indirect
	github.com/mdlayher/sdnotify v1.0.0 // indirect
	github.com/mdlayher/socket v0.4.0 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-ps v1.0.0 // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/moby/sys/mountinfo v0.6.2 // indirect
	github.com/moby/term v0.0.0-20221205130635-1aeaba878587 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/open-policy-agent/opa v0.44.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runc v1.1.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.14.0 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.40.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20200313005456-10cdbea86bc0 // indirect
	github.com/sirupsen/logrus v1.9.0 // indirect
	github.com/tabbed/pqtype v0.1.1 // indirect
	github.com/tailscale/certstore v0.1.1-0.20220316223106-78d6e1c49d8d // indirect
	github.com/tailscale/golang-x-crypto v0.0.0-20221102133106-bc99ab8c2d17 // indirect
	github.com/tailscale/goupnp v1.0.1-0.20210804011211-c64d0f06ea05 // indirect
	github.com/tailscale/netlink v1.1.1-0.20211101221916-cabfb018fe85 // indirect
	github.com/tailscale/wireguard-go v0.0.0-20221219190806-4fa124729667 // indirect
	github.com/tchap/go-patricia/v2 v2.3.1 // indirect
	github.com/tcnksm/go-httpstat v0.2.0 // indirect
	github.com/u-root/uio v0.0.0-20221213070652-c3537552635f // indirect
	github.com/valyala/fasthttp v1.44.0 // indirect
	github.com/vishvananda/netns v0.0.0-20211101163701-50045581ed74 // indirect
	github.com/vmihailenco/msgpack v4.0.4+incompatible // indirect
	github.com/vmihailenco/msgpack/v4 v4.3.12 // indirect
	github.com/vmihailenco/tagparser v0.1.1 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	github.com/yashtewari/glob-intersection v0.1.0 // indirect
	github.com/zclconf/go-cty v1.10.0 // indirect
	github.com/zeebo/errs v1.3.0 // indirect
	go.nhat.io/otelsql v0.7.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/otel v1.11.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/internal/retry v1.11.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.11.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.11.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.11.1 // indirect
	go.opentelemetry.io/otel/metric v0.33.0 // indirect
	go.opentelemetry.io/otel/sdk v1.11.1 // indirect
	go.opentelemetry.io/otel/trace v1.11.1 // indirect
	go.opentelemetry.io/proto/otlp v0.19.0 // indirect
	go4.org/mem v0.0.0-20210711025021-927187094b94 // indirect
	go4.org/netipx v0.0.0-20220725152314-7e7bdc8411bf // indirect
	golang.org/x/crypto v0.6.0 // indirect
	golang.org/x/mod v0.9.0 // indirect
	golang.org/x/oauth2 v0.5.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/term v0.6.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	golang.org/x/tools v0.7.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard/windows v0.5.3 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230223222841-637eb2293923 // indirect
	google.golang.org/grpc v1.53.0 // indirect
	google.golang.org/protobuf v1.28.2-0.20230118093459-a9481185b34d // indirect
	gopkg.in/square/go-jose.v2 v2.6.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gvisor.dev/gvisor v0.0.0-20221203005347-703fd9b7fbc0 // indirect
	inet.af/peercred v0.0.0-20210906144145-0893ea02156a // indirect
	k8s.io/klog/v2 v2.80.1 // indirect
	nhooyr.io/websocket v1.8.7 // indirect
	storj.io/drpc v0.0.33-0.20220622181519-9206537a4db7 // indirect
	tailscale.com v1.38.2 // indirect
)
