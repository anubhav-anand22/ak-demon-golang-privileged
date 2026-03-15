package main

import (
	"encoding/json"
	"fmt"
)

type BaseType struct {
	Type string `json:"type"`
}

type TestTypeMsg struct {
	Type string `json:"type"`
}
type TestMstToPriTypeMsg struct {
	Type string `json:"type"`
}
type TestMstToMobBtTypeMsg struct {
	Type string `json:"type"`
}

func ParseJsonMsgFromUnPri(jsonData []byte) (msg any, err error, defaulted bool) {
	var base BaseType
	if err := json.Unmarshal(jsonData, &base); err != nil {
		return nil, fmt.Errorf("could not peek at json type: %w", err), false
	}

	switch base.Type {
	case "TEST":
		var target TestTypeMsg
		if err := json.Unmarshal(jsonData, &target); err != nil {
			return nil, err, false
		}
		return target, nil, false
	case "SEND_TEST_MSG_TO_PRI":
		var target TestMstToPriTypeMsg
		if err := json.Unmarshal(jsonData, &target); err != nil {
			return nil, err, false
		}
		return target, nil, false
	case "SEND_TEST_MSG_TO_MOB_BT":
		var target TestMstToMobBtTypeMsg
		if err := json.Unmarshal(jsonData, &target); err != nil {
			return nil, err, false
		}
		return target, nil, false

	default:
		return nil, fmt.Errorf("unknown type: %s", base.Type), true
	}
}
