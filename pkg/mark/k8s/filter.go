package k8s

import (
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"regexp"
	"time"
)

var protectedNamespace = map[string]bool{
	"default":     true,
	"kube-system": true,
	"kube-public": true,
}

type filterable interface {
	Ignore() bool
	Compliant() bool
	GetInterface() interface{}
	GetType() string
}

func (k *K8SMarker) FilterK8SObject(f filterable) {
	if f.Ignore() {
		err := k.filterableUpdate(f.GetInterface(), f.GetType())
		if err != nil {
			k.Logger.Error(err)
		}
		return
	}
	if !f.Compliant() {
		err := k.ttlRejected(f.GetInterface(), f.GetType())
		if err != nil {
			k.Logger.Error(err)
		}
	}
}

type k8sFilterable struct {
	ignoreFilters     []Filter
	complianceFilters []Filter
	id                string
	created           time.Time
	annotations       map[string]string
	log               *logrus.Entry
	object            interface{}
	k8sObjectType     string
}

func (k *K8SMarker) newk8sFilterable(i interface{}) *k8sFilterable {
	namespace, ok := i.(v1.Namespace)
	if !ok {
		return nil
	}
	return &k8sFilterable{
		id:            namespace.Name,
		created:       namespace.CreationTimestamp.Time,
		annotations:   namespace.ObjectMeta.Annotations,
		log:           k.Logger,
		object:        i,
		k8sObjectType: "namespace",
	}
}

func (e *k8sFilterable) WithIgnoreFilter(f Filter) *k8sFilterable {
	e.ignoreFilters = append(e.ignoreFilters, f)
	return e
}

func (e *k8sFilterable) WithComplianceFilter(f Filter) *k8sFilterable {
	e.complianceFilters = append(e.complianceFilters, f)
	return e
}

func (e *k8sFilterable) GetInterface() interface{} {
	return e.object
}

func (e *k8sFilterable) GetType() string {
	return e.k8sObjectType
}

func (e *k8sFilterable) Ignore() bool {
	for _, f := range e.ignoreFilters {
		if f(e.id, e.annotations, e.created, e.log) {
			return true
		}
	}
	return false
}

func (e *k8sFilterable) Compliant() bool {
	for _, f := range e.complianceFilters {
		if f(e.id, e.annotations, e.created, e.log) {
			return false
		}
	}
	return true
}

type Filter func(id string, annotations map[string]string, created time.Time, log *logrus.Entry) bool

func ignoreProtectedNamespaceFilter(id string, annotations map[string]string, created time.Time, log *logrus.Entry) bool {
	if protectedNamespace[id] {
		log.Debugf("Ignoring %s. Reason: protected namespace", id)
		return true
	}
	return false
}

func NoTTLAnnotationFilter(id string, annotations map[string]string, created time.Time, log *logrus.Entry) bool {
	if _, exists := annotations["armory.io/bilge.ttl"]; !exists {
		log.Infof("Adding k8s namespace: %s.  Reason: no TTL annotation", id)
		return true
	}
	return false
}

func TTLExpiredFilter(id string, annotations map[string]string, created time.Time, log *logrus.Entry) bool {
	ttl := annotations["armory.io/bilge.ttl"]
	if ttl == "0" {
		log.Debugf("Ignoring %s.  Reason: Unlimted TTL", id)
		return false
	}
	if !mark.WithinTTLTime(ttl, created) {
		log.Infof("Adding namespace: %s.  Reason: TTL expired. Created on: %v", id, created)
		return true
	}
	return false
}

func (k *K8SMarker) ignoreNamespaceFilter(id string, annotations map[string]string, created time.Time, log *logrus.Entry) bool {
	for _, n := range k.Config.Not {
		if id == n {
			log.Debugf("Ignoring %s. Reason: matched ignore rule", id)
			return true
		}
	}

	for _, regex := range k.Config.NotRegex {
		// regex is already checked in config
		if regex != "" && regexp.MustCompile(regex).MatchString(id) {
			k.Logger.Debugf("Ignoring %s. Reason: matched ignore regex: %s", id, regex)
			return true
		}
	}
	return false
}
