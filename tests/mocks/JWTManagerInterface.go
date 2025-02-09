// Code generated by mockery v2.52.1. DO NOT EDIT.

package mocks

import mock "github.com/stretchr/testify/mock"

// JWTManagerInterface is an autogenerated mock type for the JWTManagerInterface type
type JWTManagerInterface struct {
	mock.Mock
}

// GetJWT provides a mock function with no fields
func (_m *JWTManagerInterface) GetJWT() string {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for GetJWT")
	}

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	return r0
}

// IsJWTValid provides a mock function with no fields
func (_m *JWTManagerInterface) IsJWTValid() (bool, error) {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for IsJWTValid")
	}

	var r0 bool
	var r1 error
	if rf, ok := ret.Get(0).(func() (bool, error)); ok {
		return rf()
	}
	if rf, ok := ret.Get(0).(func() bool); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(bool)
	}

	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// LoadJWT provides a mock function with no fields
func (_m *JWTManagerInterface) LoadJWT() error {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for LoadJWT")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func() error); ok {
		r0 = rf()
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SaveJWT provides a mock function with given fields: token
func (_m *JWTManagerInterface) SaveJWT(token string) error {
	ret := _m.Called(token)

	if len(ret) == 0 {
		panic("no return value specified for SaveJWT")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(string) error); ok {
		r0 = rf(token)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// NewJWTManagerInterface creates a new instance of JWTManagerInterface. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewJWTManagerInterface(t interface {
	mock.TestingT
	Cleanup(func())
}) *JWTManagerInterface {
	mock := &JWTManagerInterface{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
