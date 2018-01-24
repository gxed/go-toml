package toml

import (
	"fmt"
	"reflect"
	"strings"
)

type builder interface {
	enterGroupArray(key string, keys []string, position *Position) // TODO: keep just one of key or keys
	enterGroup(key string, keys []string, position *Position)      // TODO: keep just one
	enterAssign(key string, position *Position)
	foundValue(value interface{}, position *Position)

	enterArray()
	exitArray()

	enterInlineTable()
	exitInlineTable()
}

type treeBuilder struct {
	tree          *Tree // points to the root of the tree
	currentTable  []string
	seenTableKeys []string

	assignKey      string   // TODO: probably don't need me, and at least needs a better name
	assignPosition Position // TODO: same
	currentTree    *Tree    // points to the current tree being built

	inArray   bool
	array     []interface{}
	arrayType reflect.Type
}

func makeTreeBuilder() *treeBuilder {
	tree := newTree(nil)
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
	parentTree, err := b.tree.createSubTree(keys[:len(keys)-1], *position) // TODO: pass by pointer to avoid copy
	if err != nil {
		b.raiseError(position, err.Error())
	}

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
	newTree := newTree(parentTree)
	newTree.position = *position
	array = append(array, newTree)
	b.tree.SetPath(b.currentTable, array)
	b.currentTree = newTree

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

	newTree, err := b.tree.createSubTree(keys, *position)

	if err != nil {
		b.raiseError(position, "%s", err)
	}

	b.currentTree = newTree
	b.currentTable = keys
}

func (b *treeBuilder) enterAssign(key string, position *Position) {
	b.assignPosition = *position

	if b.currentTree.values[key] != nil {
		finalKey := append(b.currentTable, key)
		b.raiseError(position, "The following key was defined twice: %s", strings.Join(finalKey, "."))
	}

	b.assignKey = key
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
	b.assignTree.values[b.assignKey] = toInsert
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
		b.assignTree.values[b.assignKey] = tomlArray
		return
	}
	b.assignTree.values[b.assignKey] = &tomlValue{value: b.array, position: b.assignPosition}
	b.inArray = false
}

func (b *treeBuilder) enterInlineTable() {
	b.inlineTableTree = newTree()
}

func (b *treeBuilder) exitInlineTable() {

}
