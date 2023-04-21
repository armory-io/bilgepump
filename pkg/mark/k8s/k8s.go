package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/armory-io/bilgepump/pkg/cache"
	"github.com/armory-io/bilgepump/pkg/config"
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sync"
	"time"
)

type K8SMarker struct {
	Config    *config.Kubernetes
	Logger    *logrus.Entry
	Cache     cache.Cache
	Ctx       context.Context
	mux       *sync.Mutex
	k8sclient *kubernetes.Clientset
}

func NewK8SMarker(ctx context.Context, cfg *config.Kubernetes, logger *logrus.Logger, cache cache.Cache) (*K8SMarker, error) {

	var kconf *rest.Config
	kconf, err := clientcmd.BuildConfigFromFlags("", cfg.KubeConfig)
	if err != nil {
		return nil, err
	}

	if cfg.KubeContext != "" {
		kconf, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.KubeConfig},
			&clientcmd.ConfigOverrides{
				CurrentContext: cfg.KubeContext,
			}).ClientConfig()
		if err != nil {
			return nil, err
		}
	}

	clientset, err := kubernetes.NewForConfig(kconf)
	if err != nil {
		return nil, err
	}

	return &K8SMarker{
		Config:    cfg,
		Logger:    logger.WithFields(logrus.Fields{"class": mark.K8S, "account": cfg.Name}),
		Cache:     cache,
		Ctx:       ctx,
		mux:       &sync.Mutex{},
		k8sclient: clientset,
	}, nil
}

func (k *K8SMarker) GetMarkSchedule() string {
	return k.Config.MarkSchedule
}

func (k *K8SMarker) GetSweepSchedule() string {
	return k.Config.SweepSchedule
}

func (k *K8SMarker) GetNotifySchedule() string {
	return k.Config.NotifySchedule
}

func (k *K8SMarker) GetName() string {
	return k.Config.Name
}

func (k *K8SMarker) GetType() mark.MarkerType {
	return mark.K8S
}

func (k *K8SMarker) Mark() {
	k.Logger.Debugf("starting %s mark run for %s", mark.K8S, k.Config.Name)

	k.mux.Lock()
	defer k.mux.Unlock()
	if err := k.markNamespaces(k.Ctx); err != nil {
		k.Logger.Error(err)
	}
}

func (k *K8SMarker) Sweep() {
	k.Logger.Debugf("starting %s sweep run for %s", mark.K8S, k.Config.Name)

	k.mux.Lock()
	defer k.mux.Unlock()

	if err := k.sweepNamespaces(k.Ctx); err != nil {
		k.Logger.Error(err)
	}
}

func (k *K8SMarker) markNamespaces(ctx context.Context) error {
	namespaces, err := k.k8sclient.CoreV1().Namespaces().List(ctx, v1.ListOptions{})
	if err != nil {
		return err
	}
	for _, n := range namespaces.Items {
		k.Logger.Debugf("processing namespace: %s", n.Name)
		filterable := k.newk8sFilterable(n).
			WithIgnoreFilter(ignoreProtectedNamespaceFilter).
			WithIgnoreFilter(k.ignoreNamespaceFilter).
			WithComplianceFilter(NoTTLAnnotationFilter).
			WithComplianceFilter(TTLExpiredFilter)
		if filterable != nil {
			k.FilterK8SObject(filterable)
		}
	}
	return nil
}

func (k *K8SMarker) sweepNamespaces(ctx context.Context) error {
	owners, err := k.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := k.toDelete(o, "namespace")

		k.Logger.Debug("DryRun? ", !k.Config.DeleteEnabled)
		if len(toDelete) != 0 {
			for _, n := range toDelete {
				k.Logger.Debug("will delete ", *n)
				if k.Config.DeleteEnabled {
					if err := k.k8sclient.CoreV1().Namespaces().Delete(ctx, *n, v1.DeleteOptions{}); err != nil {
						k.Logger.Error(err)
						continue
					}
					err = mark.RemoveCandidates(o, k.Cache, []*string{n})
					if err != nil {
						k.Logger.Error(err)
					}
				}
			}
		}
	}
	return nil
}

func (k *K8SMarker) filterableUpdate(n interface{}, canType string) error {
	var owner string
	namespace, ok := n.(corev1.Namespace)
	if !ok {
		return errors.New("Cannot convert interface to v1.Namespace")
	}
	id := &namespace.Name
	owner = namespace.ObjectMeta.Annotations["armory.io/bilge.owner"]
	err := mark.RemoveCandidates(owner, k.Cache, []*string{id})
	if err != nil {
		if _, ok := err.(*mark.NoCandidatesError); !ok {
			k.Logger.Error(err)
		}
	}
	return nil
}

func (k *K8SMarker) ttlRejected(n interface{}, canType string) error {
	var (
		owner   string
		ttl     string
		purpose string
	)
	namespace, ok := n.(corev1.Namespace)
	if !ok {
		return errors.New("Cannot convert interface to v1.Namespace")
	}
	gp, _ := model.ParseDuration(k.Config.GracePeriod) // already checked this in config
	id := namespace.Name
	owner = namespace.ObjectMeta.Annotations["armory.io/bilge.owner"]
	ttl = namespace.ObjectMeta.Annotations["armory.io/bilge.ttl"]
	purpose = namespace.ObjectMeta.Annotations["armory.io/bilge.purpose"]

	marked := &mark.MarkedCandidate{
		MarkerType:    mark.K8S,
		CandidateType: canType,
		Id:            id,
		Owner:         owner,
		Purpose:       purpose,
		Ttl:           ttl,
		Account:       k.Config.Name,
	}
	mjson, err := json.Marshal(marked)
	if err != nil {
		return err
	}
	if k.Cache.CandidateExists(owner, string(mjson)) {
		k.Logger.Debugf("Instance: %s already exists in cache, skip", id)
		return nil
	}
	// owner index update
	err = k.Cache.Write("bilge:owners", owner)
	if err != nil {
		return err
	}
	// write candidate by owner
	err = k.Cache.Write(fmt.Sprintf("bilge:candidates:%s", owner), string(mjson))
	if err != nil {
		return err
	}
	// write an expiring key with our grace period
	err = k.Cache.WriteTimer(fmt.Sprintf("bilge:timers:%s", id),
		k.Config.GracePeriod, time.Now().Local().Add(time.Duration(gp)))
	if err != nil {
		return err
	}
	return nil
}

func (k *K8SMarker) toDelete(owner, thing string) []*string {
	toDelete := []*string{}
	mcs, err := mark.BuildCandidates(owner, k.Cache)
	if err != nil {
		return nil
	}
	for _, m := range mcs {
		if !k.Cache.TimerExists(fmt.Sprintf("bilge:timers:%s", m.Id)) {
			if m.CandidateType == thing && m.Account == k.Config.Name {
				k.Logger.Info("Will delete ", m.Id)
				toDelete = append(toDelete, aws.String(m.Id))
			}
		}
	}
	return toDelete
}
