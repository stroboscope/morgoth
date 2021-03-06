package config_test

import (
	"github.com/nathanielc/morgoth/Godeps/_workspace/src/github.com/stretchr/testify/assert"
	"github.com/nathanielc/morgoth/Godeps/_workspace/src/gopkg.in/yaml.v2"
	"github.com/nathanielc/morgoth/config"
	"github.com/nathanielc/morgoth/config/mocks"
	"testing"
)

type testStruct struct {
	assert    *assert.Assertions
	registery *config.Registery
}

type testConfig struct {
	A int
	B int
	C int
}

func (self *testConfig) Default() {
}

func (self testConfig) Validate() error {
	return nil
}

type testFactory struct {
}

func (self *testFactory) NewConf() config.Configuration {
	return new(testConfig)
}

func (self *testFactory) GetInstance(config config.Configuration) (interface{}, error) {
	return nil, nil
}

func (self *testStruct) UnmarshalYAML(unmarshal func(interface{}) error) error {
	engineType, config, err := config.UnmarshalDynamicType(self.registery, unmarshal)
	self.assert.Nil(err)
	self.assert.Equal("jim", engineType)
	if !self.assert.NotNil(config) {
		self.assert.Fail("Config was nil")
	}
	self.assert.Equal(testConfig{1, 2, 4}, *config.(*testConfig))
	return nil
}

func TestDynamicType(t *testing.T) {
	assert := assert.New(t)
	registery := config.NewRegistry()
	tf := testFactory{}
	registery.RegisterFactory("jim", &tf)

	ts := testStruct{assert, registery}
	var data = `
jim:
  a: 1
  b: 2
  c: 4
`
	err := yaml.Unmarshal([]byte(data), &ts)
	assert.Nil(err)

}

func TestDynamicConfiguratonShouldDefault(t *testing.T) {

	mockConf := new(mocks.Configuration)

	dc := config.DynamicConfiguration{
		Type: "test",
		Conf: mockConf,
	}

	mockConf.On("Default").Return()

	dc.Default()

	mockConf.Mock.AssertExpectations(t)

}

func TestDynamicConfiguratonShouldValidate(t *testing.T) {
	assert := assert.New(t)

	mockConf := new(mocks.Configuration)

	dc := config.DynamicConfiguration{
		Type: "test",
		Conf: mockConf,
	}

	mockConf.On("Validate").Return(nil)

	err := dc.Validate()
	assert.Nil(err)

	mockConf.Mock.AssertExpectations(t)

}

func TestDynamicConfiguratonDefaultShouldIgnoreNilConf(t *testing.T) {
	assert := assert.New(t)

	dc := config.DynamicConfiguration{
		Type: "test",
		Conf: nil,
	}

	dc.Default()
	//No panics means pass
	assert.True(true)
}

func TestDynamicConfiguratonValidateShouldFailNilConf(t *testing.T) {
	assert := assert.New(t)

	dc := config.DynamicConfiguration{
		Type: "test",
		Conf: nil,
	}

	err := dc.Validate()
	assert.NotNil(err)
}
