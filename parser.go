// TOML Parser.

package toml

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type tomlParser struct {
	flowIdx int
	flow    []token
	builder builder
}

type tomlParserStateFn func() tomlParserStateFn

// Formats and panics an error message based on a token
func (p *tomlParser) raiseError(tok *token, msg string, args ...interface{}) {
	panic(tok.Position.String() + ": " + fmt.Sprintf(msg, args...))
}

func (p *tomlParser) run() {
	for state := p.parseStart; state != nil; {
		state = state()
	}
}

func (p *tomlParser) peek() *token {
	if p.flowIdx >= len(p.flow) {
		return nil
	}
	return &p.flow[p.flowIdx]
}

func (p *tomlParser) assume(typ tokenType) {
	tok := p.getToken()
	if tok == nil {
		p.raiseError(tok, "was expecting token %s, but token stream is empty", tok)
	}
	if tok.typ != typ {
		p.raiseError(tok, "was expecting token %s, but got %s instead", typ, tok)
	}
}

func (p *tomlParser) getToken() *token {
	tok := p.peek()
	if tok == nil {
		return nil
	}
	p.flowIdx++
	return tok
}

func (p *tomlParser) parseStart() tomlParserStateFn {
	tok := p.peek()

	// end of stream, parsing is finished
	if tok == nil {
		return nil
	}

	switch tok.typ {
	case tokenDoubleLeftBracket:
		return p.parseGroupArray
	case tokenLeftBracket:
		return p.parseGroup
	case tokenKey:
		return p.parseAssign
	case tokenEOF:
		return nil
	default:
		p.raiseError(tok, "unexpected token")
	}
	return nil
}

func (p *tomlParser) parseGroupArray() tomlParserStateFn {
	startToken := p.getToken() // discard the [[
	key := p.getToken()
	if key.typ != tokenKeyGroupArray {
		p.raiseError(key, "unexpected token %s, was expecting a table array key", key)
	}

	keys, err := parseKey(key.val)
	if err != nil {
		p.raiseError(key, "invalid table array key: %s", err)
	}

	p.builder.enterGroupArray(key.val, keys, &startToken.Position)

	// move to next parser state
	p.assume(tokenDoubleRightBracket)
	return p.parseStart
}

func (p *tomlParser) parseGroup() tomlParserStateFn {
	startToken := p.getToken() // discard the [
	key := p.getToken()
	if key.typ != tokenKeyGroup {
		p.raiseError(key, "unexpected token %s, was expecting a table key", key)
	}

	keys, err := parseKey(key.val)
	if err != nil {
		p.raiseError(key, "invalid table array key: %s", err)
	}

	p.builder.enterGroup(key.val, keys, &startToken.Position)

	p.assume(tokenRightBracket)
	return p.parseStart
}

func (p *tomlParser) parseAssign() tomlParserStateFn {
	key := p.getToken()
	p.assume(tokenEqual)

	p.builder.enterAssign(key.val, &key.Position)

	p.parseRvalue()

	// p.exitAssign() TODO: maybe?

	return p.parseStart
}

var numberUnderscoreInvalidRegexp *regexp.Regexp
var hexNumberUnderscoreInvalidRegexp *regexp.Regexp

func numberContainsInvalidUnderscore(value string) error {
	if numberUnderscoreInvalidRegexp.MatchString(value) {
		return errors.New("invalid use of _ in number")
	}
	return nil
}

func hexNumberContainsInvalidUnderscore(value string) error {
	if hexNumberUnderscoreInvalidRegexp.MatchString(value) {
		return errors.New("invalid use of _ in hex number")
	}
	return nil
}

func cleanupNumberToken(value string) string {
	cleanedVal := strings.Replace(value, "_", "", -1)
	return cleanedVal
}

func (p *tomlParser) parseRvalue() interface{} {
	tok := p.getToken()
	if tok == nil || tok.typ == tokenEOF {
		p.raiseError(tok, "expecting a value")
	}

	switch tok.typ {
	case tokenString:
		p.builder.foundValue(tok.val, &tok.Position)
		return tok.val
	case tokenTrue:
		p.builder.foundValue(true, &tok.Position)
		return true
	case tokenFalse:
		p.builder.foundValue(false, &tok.Position)
		return false
	case tokenInf:
		if tok.val[0] == '-' {
			p.builder.foundValue(math.Inf(-1), &tok.Position)
			return math.Inf(-1)
		}
		p.builder.foundValue(math.Inf(1), &tok.Position)
		return math.Inf(1)
	case tokenNan:
		p.builder.foundValue(math.NaN(), &tok.Position)
		return math.NaN()
	case tokenInteger:
		cleanedVal := cleanupNumberToken(tok.val)
		var err error
		var val int64
		if len(cleanedVal) >= 3 && cleanedVal[0] == '0' {
			switch cleanedVal[1] {
			case 'x':
				err = hexNumberContainsInvalidUnderscore(tok.val)
				if err != nil {
					p.raiseError(tok, "%s", err)
				}
				val, err = strconv.ParseInt(cleanedVal[2:], 16, 64)
			case 'o':
				err = numberContainsInvalidUnderscore(tok.val)
				if err != nil {
					p.raiseError(tok, "%s", err)
				}
				val, err = strconv.ParseInt(cleanedVal[2:], 8, 64)
			case 'b':
				err = numberContainsInvalidUnderscore(tok.val)
				if err != nil {
					p.raiseError(tok, "%s", err)
				}
				val, err = strconv.ParseInt(cleanedVal[2:], 2, 64)
			default:
				panic("invalid base") // the lexer should catch this first
			}
		} else {
			err = numberContainsInvalidUnderscore(tok.val)
			if err != nil {
				p.raiseError(tok, "%s", err)
			}
			val, err = strconv.ParseInt(cleanedVal, 10, 64)
		}
		if err != nil {
			p.raiseError(tok, "%s", err)
		}
		p.builder.foundValue(val, &tok.Position)
		return val
	case tokenFloat:
		err := numberContainsInvalidUnderscore(tok.val)
		if err != nil {
			p.raiseError(tok, "%s", err)
		}
		cleanedVal := cleanupNumberToken(tok.val)
		val, err := strconv.ParseFloat(cleanedVal, 64)
		if err != nil {
			p.raiseError(tok, "%s", err)
		}
		p.builder.foundValue(val, &tok.Position)
		return val
	case tokenDate:
		val, err := time.ParseInLocation(time.RFC3339Nano, tok.val, time.UTC)
		if err != nil {
			p.raiseError(tok, "%s", err)
		}
		p.builder.foundValue(val, &tok.Position)
		return val
	case tokenLeftBracket:
		p.parseArray()
		return nil
	case tokenLeftCurlyBrace:
		return p.parseInlineTable() // TODO: next up
	case tokenEqual:
		p.raiseError(tok, "cannot have multiple equals for the same key")
	case tokenError:
		p.raiseError(tok, "%s", tok)
	}

	p.raiseError(tok, "never reached")

	return nil
}

func tokenIsComma(t *token) bool {
	return t != nil && t.typ == tokenComma
}

func (p *tomlParser) parseInlineTable() *Tree {
	p.builder.enterInlineTable()
	var previous *token
Loop:
	for {
		follow := p.peek()
		if follow == nil || follow.typ == tokenEOF {
			p.raiseError(follow, "unterminated inline table")
		}
		switch follow.typ {
		case tokenRightCurlyBrace:
			p.getToken()
			break Loop
		case tokenKey:
			if !tokenIsComma(previous) && previous != nil {
				p.raiseError(follow, "comma expected between fields in inline table")
			}
			key := p.getToken()
			p.assume(tokenEqual)
			value := p.parseRvalue()
			tree.Set(key.val, value)
		case tokenComma:
			if previous == nil {
				p.raiseError(follow, "inline table cannot start with a comma")
			}
			if tokenIsComma(previous) {
				p.raiseError(follow, "need field between two commas in inline table")
			}
			p.getToken()
		default:
			p.raiseError(follow, "unexpected token type in inline table: %s", follow.String())
		}
		previous = follow
	}
	if tokenIsComma(previous) {
		p.raiseError(previous, "trailing comma at the end of inline table")
	}
	return tree
}

func (p *tomlParser) parseArray() {
	p.builder.enterArray()

	for {
		follow := p.peek()
		if follow == nil || follow.typ == tokenEOF {
			p.raiseError(follow, "unterminated array")
		}
		if follow.typ == tokenRightBracket {
			p.getToken()
			break
		}

		p.parseRvalue()

		follow = p.peek()
		if follow == nil || follow.typ == tokenEOF {
			p.raiseError(follow, "unterminated array")
		}
		if follow.typ != tokenRightBracket && follow.typ != tokenComma {
			p.raiseError(follow, "missing comma")
		}
		if follow.typ == tokenComma {
			p.getToken()
		}
	}
	p.builder.exitArray()
}

func parseToml(flow []token) *Tree {
	builder := makeTreeBuilder()
	parser := &tomlParser{
		flowIdx: 0,
		flow:    flow,
		builder: builder,
	}
	parser.run()
	return builder.tree
}

func init() {
	numberUnderscoreInvalidRegexp = regexp.MustCompile(`([^\d]_|_[^\d])|_$|^_`)
	hexNumberUnderscoreInvalidRegexp = regexp.MustCompile(`(^0x_)|([^\da-f]_|_[^\da-f])|_$|^_`)
}
