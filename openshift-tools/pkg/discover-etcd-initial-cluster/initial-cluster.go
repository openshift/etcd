package discover_etcd_initial_cluster

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"

	"github.com/spf13/cobra"
)

type DiscoverEtcdInitialClusterOptions struct {
	// TargetPeerURLHost is the host portion of the peer URL.  It is used to match on. (either IP or hostname)
	TargetPeerURLHost string

	// CABundleFile is the file to use to trust the etcd server
	CABundleFile string
	// ClientCertFile is the client cert to use to authenticate this binary to etcd
	ClientCertFile string
	// ClientKeyFile is the client key to use to authenticate this binary to etcd
	ClientKeyFile string
	// Endpoints is a list of all the endpoints to use to try to contact etcd
	Endpoints string

	// Revision is the revision value for the static pod
	Revision string
	// PreviousEtcdInitialClusterDir is the directory to store the previous etcd initial cluster value
	PreviousEtcdInitialClusterDir string

	// TotalTimeToWait is the total time to wait before reporting failure and dumping logs
	TotalTimeToWait time.Duration
	// TimeToWaitBeforeUsingPreviousValue is the time to wait before checking to see if we have a previous value.
	TimeToWaitBeforeUsingPreviousValue time.Duration
}

func NewDiscoverEtcdInitialCluster() *DiscoverEtcdInitialClusterOptions {
	return &DiscoverEtcdInitialClusterOptions{
		TotalTimeToWait:                    30 * time.Second,
		TimeToWaitBeforeUsingPreviousValue: 10 * time.Second,
	}
}

func NewDiscoverEtcdInitialClusterCommand() *cobra.Command {
	o := NewDiscoverEtcdInitialCluster()

	cmd := &cobra.Command{
		Use:   "discover-etcd-initial-cluster",
		Short: "output the value for ETCD_INITIAL_CLUSTER in openshift etcd static pod",
		Long: `output the value for ETCD_INITIAL_CLUSTER in openshift etcd static pod

1. It tries to contact every available etcd to get a list of member.
2. Check each member to see if any one of them is the target.
3. If so, and if it is started, use the member list to create the ETCD_INITIAL_CLUSTER value and print it out.
4. If so, and if it it not started, use the existing member list and append the target value to create the ETCD_INITIAL_CLUSTER value and print it out.
5. If not, try again until either you have it or you have to check a cache.
6. If you have to check a cache and it is present, return
7. If the cache is not present, keep trying to contact etcd until total timeout is met.
`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := o.Validate(); err != nil {
				fmt.Fprint(os.Stderr, err)
				os.Exit(1)
			}

			if err := o.Run(); err != nil {
				fmt.Fprint(os.Stderr, err)
				os.Exit(1)
			}
		},
	}
	o.BindFlags(cmd.Flags())

	return cmd
}

func (o *DiscoverEtcdInitialClusterOptions) BindFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.CABundleFile, "cacert", o.CABundleFile, "file to use to verify the identity of the etcd server")
	flags.StringVar(&o.ClientCertFile, "cert", o.ClientCertFile, "client cert to use to authenticate this binary to etcd")
	flags.StringVar(&o.ClientKeyFile, "key", o.ClientKeyFile, "client key to use to authenticate this binary to etcd")
	flags.StringVar(&o.Endpoints, "endpoints", o.Endpoints, "list of all the endpoints to use to try to contact etcd")
	flags.StringVar(&o.Revision, "revision", o.Revision, "revision value for the static pod")
	flags.StringVar(&o.PreviousEtcdInitialClusterDir, "memory-dir", o.PreviousEtcdInitialClusterDir, "directory to store the previous etcd initial cluster value")
}

func (o *DiscoverEtcdInitialClusterOptions) Validate() error {
	return nil
}

func (o *DiscoverEtcdInitialClusterOptions) Run() error {
	return nil
}
