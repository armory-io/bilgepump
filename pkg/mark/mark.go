package mark

import (
	"encoding/json"
	"fmt"
	"github.com/armory-io/bilgepump/pkg/cache"
	"github.com/nlopes/slack"
	"github.com/prometheus/common/model"
	"log"
	"time"
)

type Marker interface {
	Mark()
	Sweep()
	GetMarkSchedule() string
	GetSweepSchedule() string
	GetNotifySchedule() string
	GetName() string
	GetType() MarkerType
}

type NoCandidatesError struct {
	err string
}

func (e *NoCandidatesError) Error() string {
	return e.err
}

type MarkerType int

const (
	AWS          MarkerType = 0
	GCP          MarkerType = 1
	K8S          MarkerType = 2
	REQUIRED_TAG            = "ttl"
)

func (mt MarkerType) String() string {
	strRep := [...]string{
		"AWS",
		"GCP",
		"K8S",
	}
	return strRep[mt]
}

func (mt MarkerType) Color() string {
	color := [...]string{
		"#F4D03F", // yellow
		"#FF0000", // red
		"#0000FF", // blue
	}
	return color[mt]
}

type MarkedCandidate struct {
	MarkerType    MarkerType        `json:"marker_type"`
	CandidateType string            `json:"candidate_type"`
	Id            string            `json:"id"`
	Owner         string            `json:"owner"`
	Ttl           string            `json:"ttl"`
	Purpose       string            `json:"purpose"`
	Account       string            `json:"account"`
	Tags          map[string]string `json:"tags"`
}

func (mc *MarkedCandidate) GenerateSlackAttachmentFields() []slack.AttachmentField {
	afs := []slack.AttachmentField{}

	for k, v := range mc.Tags {
		afs = append(afs, slack.AttachmentField{
			Title: k,
			Value: v,
			Short: true,
		})
	}

	afs = append(afs, slack.AttachmentField{
		Title: "purpose",
		Value: mc.Purpose,
		Short: true,
	})
	afs = append(afs, slack.AttachmentField{
		Title: "owner",
		Value: mc.Owner,
		Short: true,
	})
	afs = append(afs, slack.AttachmentField{
		Title: "type",
		Value: mc.CandidateType,
		Short: true,
	})
	afs = append(afs, slack.AttachmentField{
		Title: "account",
		Value: mc.Account,
		Short: true,
	})
	return afs
}

func WithinTTLTime(ttl string, start time.Time) bool {
	parsedTtl, err := model.ParseDuration(ttl)
	if err != nil {
		return false
	}
	since := time.Since(start)
	// if the time duration since we've started is greater than or equal to our ttl duration
	// then our time has expired
	return since < time.Duration(parsedTtl)
}

func BuildCandidates(owner string, c cache.Cache) ([]*MarkedCandidate, error) {
	cans := c.ReadCandidates(owner)
	if len(cans) == 0 {
		return nil, &NoCandidatesError{"no candidates to mark"}
	}
	mcs := make([]*MarkedCandidate, len(cans))
	for i, mc := range cans {
		var m *MarkedCandidate
		err := json.Unmarshal([]byte(mc), &m)
		if err != nil {
			continue
		}
		mcs[i] = m
	}
	return mcs, nil
}

func RemoveCandidates(owner string, c cache.Cache, ids []*string) error {
	strCans := c.ReadCandidates(owner)
	if len(strCans) == 0 {
		return &NoCandidatesError{"no candidates to remove"}
	}
	cans, err := BuildCandidates(owner, c)
	if err != nil {
		return err
	}
	for ix, can := range cans {
		for _, v := range ids {
			value := *v
			if value == can.Id {
				err = c.Delete(fmt.Sprintf("bilge:candidates:%s", owner), strCans[ix])
				if err != nil {
					log.Printf("WARNING: Error deleting candidate.  Owner:%s, Candidate: %s, Error:%+v\n", owner, strCans[ix], err)
				}
			}
		}
	}

	return nil
}
