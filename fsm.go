package gofsm

import (
	"context"
	"fmt"
	"os/exec"
	"qiniupkg.com/x/errors.v7"
	"runtime"
	"strings"
)

type State  string
type Event  string
type Action func(ctx context.Context, from State, event Event, to []State) (State, error)
type StatesDef map[State]string
type EventsDef map[Event]string
type EventProcessor interface {
	OnExit(ctx context.Context, state State, event Event) error
	OnActionFailure(ctx context.Context, from State, event Event, to []State, err error) error
	OnEnter(ctx context.Context, state State) error
}
type Transition struct {
	From      State
	Event     Event
	To        []State
	Action    Action
	Processor EventProcessor
}

/**
状态机执行表述图
有限状态机
	- 确定状态机
	- 非确定状态机
*/
type stateGraph struct {
	name        string // 状态图名称
	start       []State
	end         []State
	states      StatesDef
	events      EventsDef
	transitions map[State]map[Event]*Transition
}

/**
状态机
*/
type StateMachine struct {
	processor EventProcessor
	sg        *stateGraph
}

/**
默认实现
*/
type DefaultProcessor struct{}

func (*DefaultProcessor) OnExit(ctx context.Context, state State, event Event) error {
	//log.Printf("exit [%s]", state)
	return nil
}

func (*DefaultProcessor) OnActionFailure(ctx context.Context, from State, event Event, to []State, err error) error {
	//log.Printf("failure %s -(%s)-> [%s]: (%s)", from, event, strings.Join(to, "|"), err.Error())
	return nil
}

func (*DefaultProcessor) OnEnter(ctx context.Context, state State) error {
	//log.Printf("enter [%s]", state)
	return nil
}

/**
默认值定义
*/
const Start = "[*]"
const End = "[*]"
const None = ""

var NoopAction Action = func(ctx context.Context, from State, event Event, to []State) (State, error) {
	if to == nil || len(to) == 0 {
		return None, nil
	}
	return to[0], nil
}
var NoopProcessor = &DefaultProcessor{}

/**
创建一个状态机执行器
*/
func New(name string) *StateMachine {
	return (&StateMachine{
		sg: &stateGraph{
			transitions: map[State]map[Event]*Transition{},
		}}).Name(name)
}

/**
设置所有状态
*/
func (sm *StateMachine) States(states StatesDef) *StateMachine {
	sm.sg.states = states
	return sm
}

/**
设置所有时间
*/
func (sm *StateMachine) Events(events EventsDef) *StateMachine {
	sm.sg.events = events
	return sm
}

func (sm *StateMachine) Name(s string) *StateMachine {
	sm.sg.name = s
	return sm
}

func (sm *StateMachine) Start(start []State) *StateMachine {
	sm.sg.start = start
	return sm
}

func (sm *StateMachine) End(end []State) *StateMachine {
	sm.sg.end = end
	return sm
}

func (sm *StateMachine) Processor(processor EventProcessor) *StateMachine {
	sm.processor = processor
	return sm
}

/**
添加状态转换
TODO 不确定状态机，多个 Action 如何处理 ？？？
*/
func (sm *StateMachine) Transitions(transitions ...Transition) *StateMachine {
	for index := range transitions {
		newTransfer := &transitions[index]
		events, ok := sm.sg.transitions[newTransfer.From]
		if !ok {
			events = map[Event]*Transition{}
			sm.sg.transitions[newTransfer.From] = events
		}
		if transfer, ok := events[newTransfer.Event]; ok {
			transfer.To = append(transfer.To, newTransfer.To...)
			// 去掉重复
			//sort.Strings(transfer.To)
			transfer.To = removeRepByMap(transfer.To)
			events[newTransfer.Event] = transfer
		} else {
			events[newTransfer.Event] = newTransfer
		}
	}
	return sm
}

//slice去重
func removeRepByMap(slc []State) []State {
	result := []State{}         //存放返回的不重复切片
	tempMap := map[State]byte{} // 存放不重复主键
	for _, e := range slc {
		l := len(tempMap)
		tempMap[e] = 0 //当e存在于tempMap中时，再次添加是添加不进去的，，因为key不允许重复
		//如果上一行添加成功，那么长度发生变化且此时元素一定不重复
		if len(tempMap) != l { // 加入map后，map长度变化，则元素不重复
			result = append(result, e) //当元素不重复时，将元素添加到切片result中
		}
	}
	return result
}
func removeDuplicatesAndEmpty(a []State) (ret []State) {
	aLen := len(a)
	for i := 0; i < aLen; i++ {
		if (i > 0 && a[i-1] == a[i]) || len(a[i]) == 0 {
			continue
		}
		ret = append(ret, a[i])
	}
	return
}

/**
触发状态转换
*/
func (sm *StateMachine) Trigger(ctx context.Context, from State, event Event) (State, error) {
	if _, ok := sm.sg.states[from]; !ok {
		return "", errors.New(fmt.Sprintf("状态机不包含状态%s", from))
	}
	if _, ok := sm.sg.events[event]; !ok {
		return "", errors.New(fmt.Sprintf("状态机不包含事件 " , event))
	}
	if transfer, ok := sm.sg.transitions[from][event]; ok {

		processor := sm.processor
		// 离开状态处理，转换之前
		if transfer.Processor != nil {
			processor = transfer.Processor
		}
		if processor == nil {
			processor = NoopProcessor
		}

		_ = processor.OnExit(ctx, from, event)

		to, err := transfer.Action(ctx, from, event, transfer.To)
		if err != nil {
			// 转换执行错误处理
			_ = processor.OnActionFailure(ctx, from, event, transfer.To, err)
			return to, err
		}
		// TODO 返回状态不在状态表中如何处理 ？？？

		// 进入状态处理，转换之后
		_ = processor.OnEnter(ctx, to)

		return to, err
	}
	return "", errors.New(fmt.Sprintf("没有定义状态转换事件 [%v --%v--> ???]", from, event))

}

/**
输出图的显示内容
输出 PlantUML 显示 URL
*/
func (sm *StateMachine) Show() string {
	return sm.sg.show()
}

func (transfer Transition) String() string {
	return fmt.Sprintf("%s --> %s: %s", transfer.From, transfer.To, transfer.Event)
}

/**
输出图的显示内容
输出 PlantUML 显示 URL
*/
func (sg *stateGraph) show() string {
	// 头部信息
	title := ""
	smType := "DFA"
	if sg.name != "" {
		title = "<b>[" + sg.name + "]</b> "
	}

	// 状态的定义
	var stateLines []string
	for state, desc := range sg.states {
		stateLine := string(state)

		nextNFA := ""
		for _, transfer := range sg.transitions[state] {
			if len(transfer.To) > 1 {
				nextNFA = "<<NFA>>"
			}
		}

		if desc != "" {
			stateLine = fmt.Sprintf(`state "%s" as %s %s :%s`, state, state, nextNFA, desc)
		} else {
			stateLine = fmt.Sprintf(`state "%s" as %s %s`, state, state, nextNFA)
		}

		stateLines = append(stateLines, stateLine)
	}
	statesDef := strings.Join(stateLines, "\n")

	// 状态转换描述
	var transferLines []string
	// 开始状态处理
	if sg.start != nil && len(sg.start) > 0 {
		for _, event := range sg.start {
			transferLines = append(transferLines,
				fmt.Sprintf("%s --> %s",
					Start,
					event))
		}
	}
	// 处理中间状态转换
	for from, events := range sg.transitions {
		for event, transfer := range events {
			eventString := fmt.Sprintf("%s",event)
			if len(transfer.To) > 1 {
				smType = "NFA"
			}
			if eventString != "" {
				desc := sg.events[event]
				eventString = "(" + eventString + ") "
				if desc != "" {
					eventString = eventString + fmt.Sprintf("%s",desc)
				}
				if len(transfer.To) > 1 {
					eventString = "<font color=red><b>" + eventString + "</b></font>"
				}
			}
			// plantUml 格式
			if eventString != "" {
				eventString = ": " + eventString
			}

			for j := 0; j < len(transfer.To); j++ {
				to := transfer.To[j]
				transferLines = append(transferLines,
					fmt.Sprintf("%s --> %s %s",
						from,
						to,
						eventString))
			}
		}
	}
	// 结束状态处理
	if sg.end != nil && len(sg.end) > 0 {
		for _, event := range sg.end {
			transferLines = append(transferLines,
				fmt.Sprintf("%s --> %s",
					event,
					End))
		}
	}
	transitionsDef := strings.Join(transferLines, "\n")

	// 生成 plantUml script
	raw := `
	@startuml
	skinparam state {
	  BackgroundColor<<NFA>> Red
	}
	State "<font color=red><b><<%s>></b></font>\n` + title + `State Graph" as rootGraph {
		%s

		%s
	}

	@enduml
	`
	raw = fmt.Sprintf(raw, smType, statesDef, transitionsDef)

	// 输出 plantUml 和 在线生成图标地址
	plantText := encode(raw)
	imgUrl := "https://www.plantuml.com/plantuml/img/~1" + plantText
	svgUrl := "https://www.plantuml.com/plantuml/svg/~1" + plantText
	format := "\nPlantUml Script:\n%s\n\nOnline Graph:\n\tImg: %s\n\tSvg: %s"
	open(imgUrl)
	return fmt.Sprintf(format, raw, imgUrl, svgUrl)
}
func open(url string) error {
    var cmd string
    var args []string

    switch runtime.GOOS {
    case "windows":
        cmd = "cmd"
        args = []string{"/c", "start"}
    case "darwin":
        cmd = "open"
    default: // "linux", "freebsd", "openbsd", "netbsd"
        cmd = "xdg-open"
    }
    args = append(args, url)
    return exec.Command(cmd, args...).Start()
}
