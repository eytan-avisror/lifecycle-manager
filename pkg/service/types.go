package service

import (
	"reflect"
	"sync"

	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type Authenticator struct {
	ScalingGroupClient autoscalingiface.AutoScalingAPI
	SQSClient          sqsiface.SQSAPI
	KubernetesClient   kubernetes.Interface
}

type Manager struct {
	eventStream   chan *sqs.Message
	authenticator Authenticator
	context       ManagerContext
	queue         []LifecycleEvent
	queueSync     *sync.Mutex
}

func New(auth Authenticator, ctx ManagerContext) *Manager {
	return &Manager{
		eventStream:   make(chan *sqs.Message, 0),
		queue:         make([]LifecycleEvent, 0),
		queueSync:     &sync.Mutex{},
		authenticator: auth,
		context:       ctx,
	}
}

func (g *Manager) AddEvent(event LifecycleEvent) {
	g.queueSync.Lock()
	g.queue = append(g.queue, event)
	g.queueSync.Unlock()
}

func (g *Manager) CompleteEvent(event LifecycleEvent) {
	newQueue := make([]LifecycleEvent, 0)
	for _, e := range g.queue {
		if reflect.DeepEqual(event, e) {
			log.Debugf("event %v completed processing", event.RequestID)
		} else {
			newQueue = append(newQueue, e)
		}
	}
	g.queueSync.Lock()
	g.queue = newQueue
	g.queueSync.Unlock()
}

type ManagerContext struct {
	KubectlLocalPath          string
	QueueName                 string
	Region                    string
	DrainTimeoutSeconds       int64
	DrainRetryIntervalSeconds int64
	PollingIntervalSeconds    int64
}

type LifecycleEvent struct {
	LifecycleHookName    string `json:"LifecycleHookName"`
	AccountID            string `json:"AccountId"`
	RequestID            string `json:"RequestId"`
	LifecycleTransition  string `json:"LifecycleTransition"`
	AutoScalingGroupName string `json:"AutoScalingGroupName"`
	EC2InstanceID        string `json:"EC2InstanceId"`
	LifecycleActionToken string `json:"LifecycleActionToken"`
	receiptHandle        string
	queueURL             string
	heartbeatInterval    int64
	referencedNode       v1.Node
	drainCompleted       bool
}

func (e *LifecycleEvent) IsValid() bool {
	if e.LifecycleTransition != TerminationEventName {
		log.Warnf("got unsupported event type: '%+v'", e.LifecycleTransition)
		return false
	}

	if e.EC2InstanceID == "" {
		log.Warnf("instance-id not provided in event: %+v", e)
		return false
	}

	if e.LifecycleHookName == "" {
		log.Warnf("hook-name not provided in event: %+v", e)
		return false
	}

	return true
}

func (e *LifecycleEvent) IsAlreadyExist(queue []LifecycleEvent) bool {
	for _, event := range queue {
		if event.RequestID == e.RequestID {
			log.Debugf("event %v already being processed, discarding", event.RequestID)
			return true
		}
	}
	return false
}

// SetReceiptHandle is a setter method for the receipt handle of the event
func (e *LifecycleEvent) SetReceiptHandle(receipt string) { e.receiptHandle = receipt }

// SetQueueURL is a setter method for the url of the SQS queue
func (e *LifecycleEvent) SetQueueURL(url string) { e.queueURL = url }

// SetHeartbeatInterval is a setter method for heartbeat interval of the event
func (e *LifecycleEvent) SetHeartbeatInterval(interval int64) { e.heartbeatInterval = interval }

// SetReferencedNode is a setter method for the event referenced node
func (e *LifecycleEvent) SetReferencedNode(node v1.Node) { e.referencedNode = node }

// SetDrainCompleted is a setter method for status of the drain operation
func (e *LifecycleEvent) SetDrainCompleted(val bool) { e.drainCompleted = val }
