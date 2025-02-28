// Code generated by mockery v2.52.1. DO NOT EDIT.

package mocks

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	mock "github.com/stretchr/testify/mock"
)

// MQTTClient is an autogenerated mock type for the MQTTClient type
type MQTTClient struct {
	mock.Mock
}

// Connect provides a mock function with no fields
func (_m *MQTTClient) Connect() mqtt.Token {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for Connect")
	}

	var r0 mqtt.Token
	if rf, ok := ret.Get(0).(func() mqtt.Token); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(mqtt.Token)
		}
	}

	return r0
}

// Disconnect provides a mock function with given fields: quiesce
func (_m *MQTTClient) Disconnect(quiesce uint) {
	_m.Called(quiesce)
}

// Publish provides a mock function with given fields: topic, qos, retained, payload
func (_m *MQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	ret := _m.Called(topic, qos, retained, payload)

	if len(ret) == 0 {
		panic("no return value specified for Publish")
	}

	var r0 mqtt.Token
	if rf, ok := ret.Get(0).(func(string, byte, bool, interface{}) mqtt.Token); ok {
		r0 = rf(topic, qos, retained, payload)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(mqtt.Token)
		}
	}

	return r0
}

// Subscribe provides a mock function with given fields: topic, qos, callback
func (_m *MQTTClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	ret := _m.Called(topic, qos, callback)

	if len(ret) == 0 {
		panic("no return value specified for Subscribe")
	}

	var r0 mqtt.Token
	if rf, ok := ret.Get(0).(func(string, byte, mqtt.MessageHandler) mqtt.Token); ok {
		r0 = rf(topic, qos, callback)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(mqtt.Token)
		}
	}

	return r0
}

// Unsubscribe provides a mock function with given fields: topics
func (_m *MQTTClient) Unsubscribe(topics ...string) mqtt.Token {
	_va := make([]interface{}, len(topics))
	for _i := range topics {
		_va[_i] = topics[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	if len(ret) == 0 {
		panic("no return value specified for Unsubscribe")
	}

	var r0 mqtt.Token
	if rf, ok := ret.Get(0).(func(...string) mqtt.Token); ok {
		r0 = rf(topics...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(mqtt.Token)
		}
	}

	return r0
}

// NewMQTTClient creates a new instance of MQTTClient. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMQTTClient(t interface {
	mock.TestingT
	Cleanup(func())
}) *MQTTClient {
	mock := &MQTTClient{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
