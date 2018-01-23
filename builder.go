package toml

import (
	"fmt"
	"strings"
	"reflect"
)

type builder interface {
	enterGroupArray(key string, keys []string, position *Position) // TODO: keep just one of key or keys
	enterGroup(key string, keys []string, position *Position)      // TODO: keep just one
	enterAssign(key string, position *Position)
	foundValue(value interface{}, position *Position)
	enterArray()
	exitArray()
	enterInlineTable()
}

type treeBuilder struct {
	tree          *Tree
	currentTable  []string
	seenTableKeys []string

	assignKeyVal string // TODO: probably don't need me, and at least needs a better name
	assignPosition Position // TODO: same
	assignTree *Tree

	inArray bool
	array []interface{}
	arrayType reflect.Type

	inlineTableTree *Tree
}

func makeTreeBuilder() *treeBuilder {
	tree := newTree()
	tree.position = Position{1, 1}
	return &treeBuilder{
		tree:          tree,
		currentTable:  make([]string, 0),
		seenTableKeys: make([]string, 0),
	}
}

func (b *treeBuilder) raiseError(position *Position, msg string, args ...interface{}) {
	panic(position.String() + ": " + fmt.Sprintf(msg, args...))
}

func (b *treeBuilder) enterGroupArray(key string, keys []string, position *Position) {
	// get or create table array element at the indicated part in the path
	b.tree.createSubTree(keys[:len(keys)-1], *position) // TODO: pass by pointer to avoid copy
	destTree := b.tree.GetPath(keys)
	var array []*Tree
	if destTree == nil {
		array = make([]*Tree, 0)
	} else if target, ok := destTree.([]*Tree); ok && target != nil {
		array = destTree.([]*Tree)
	} else {
		b.raiseError(position, "key %s is already assigned and not of type table array", key)
	}
	b.currentTable = keys

	// add a new tree to the end of the table array
	newTree := newTree()
	newTree.position = *position
	array = append(array, newTree)
	b.tree.SetPath(b.currentTable, array)

	// remove all keys that were children of this table array
	prefix := key + "."
	found := false
	for ii := 0; ii < len(b.seenTableKeys); {
		tableKey := b.seenTableKeys[ii]
		if strings.HasPrefix(tableKey, prefix) {
			b.seenTableKeys = append(b.seenTableKeys[:ii], b.seenTableKeys[ii+1:]...)
		} else {
			found = tableKey == key
			ii++
		}
	}

	// keep this key name from use by other kinds of assignments
	if !found {
		b.seenTableKeys = append(b.seenTableKeys, key)
	}
}

func (b *treeBuilder) enterGroup(key string, keys []string, position *Position) {
	for _, item := range b.seenTableKeys {
		if item == key {
			b.raiseError(position, "duplicated tables")
		}
	}

	b.seenTableKeys = append(b.seenTableKeys, key)

	if err := b.tree.createSubTree(keys, *position); err != nil {
		b.raiseError(position, "%s", err)
	}

	b.currentTable = keys
}

func (b *treeBuilder) enterAssign(key string, position *Position) {
	b.assignPosition = *position

	var tableKey []string
	if len(b.currentTable) > 0 {
		tableKey = b.currentTable
	} else {
		tableKey = []string{}
	}

	// find the table to assign, looking out for arrays of tables
	var targetNode *Tree
	switch node := b.tree.GetPath(tableKey).(type) {
	case []*Tree:
		targetNode = node[len(node)-1]
	case *Tree:
		targetNode = node
	default:
		b.raiseError(position, "Unknown table type for path: %s", strings.Join(tableKey, "."))
	}

	keyVals := []string{b.assignKeyVal}
	if len(keyVals) != 1 { // TODO: this test is suspicious
		b.raiseError(position, "Invalid key")
	}
	keyVal := keyVals[0]
	localKey := []string{keyVal}
	finalKey := append(tableKey, keyVal)
	if targetNode.GetPath(localKey) != nil {
		b.raiseError(position, "The following key was defined twice: %s", strings.Join(finalKey, "."))
	}

	b.assignKeyVal = keyVal
	b.assignTree = targetNode
}

func (b *treeBuilder) foundValue(value interface{}, position *Position) {
	if b.inArray {
		if b.arrayType == nil {
			b.arrayType = reflect.TypeOf(value)
		}
		if reflect.TypeOf(value) != b.arrayType {
			b.raiseError(position, "mixed types in array")
		}
		b.array = append(b.array, value)
		return
	}

	var toInsert interface{}
	switch value.(type) {
	case *Tree, []*Tree:
		toInsert = value
	default:
		toInsert = &tomlValue{value: value, position: b.assignPosition}
	}
	b.assignTree.values[b.assignKeyVal] = toInsert
}

func (b *treeBuilder) enterArray() {
	b.array = make([]interface{}, 0)
	b.arrayType = reflect.TypeOf(nil)
	b.inArray = true
}

func (b *treeBuilder) exitArray() {
	// An array of Trees is actually an array of inline
	// tables, which is a shorthand for a table array. If the
	// array was not converted from []interface{} to []*Tree,
	// the two notations would not be equivalent.
	if b.arrayType == reflect.TypeOf(newTree()) {
		tomlArray := make([]*Tree, len(b.array))
		for i, v := range b.array {
			tomlArray[i] = v.(*Tree)
		}
		b.assignTree.values[b.assignKeyVal] = tomlArray
		return
	}
	b.assignTree.values[b.assignKeyVal] = &tomlValue{value: b.array, position: b.assignPosition}
	b.inArray = false
}

func (b *treeBuilder) enterInlineTable() {
	b.inlineTableTree = newTree()
}