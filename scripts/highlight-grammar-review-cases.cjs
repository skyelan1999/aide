"use strict";

const cases = [ [ 269, "kotlin", "/* outer /* middle /* inner */ val inner = 1 */ val stillComment = 2 */ val outside = 3", "val stillComment = 2", "comment" ], [ 270, "bash", 'echo $(date)\necho "$(echo $(id))"', "$(echo $(id))", "subst" ], [ 271, "vim", '" comment with "quoted" words\nlet s = "actual string"', '" comment with "quoted" words', "comment" ], [ 272, "csharp", 'var text = $"value: {new List<int> { 1, 2 }.Count}";', "{new List<int> { 1, 2 }.Count}", "subst" ], [ 273, "graphql", 'description: """a "quoted" value"""', 'a "quoted" value', "string" ], [ 274, "go", "func f(cb func(int, string)) { return }", "(cb func(int, string))", "params" ], [ 275, "cmake", "#[=[ comment ]==] still comment ]=]\nset(X [=[ text ]==] still text ]=])", "still comment", "comment" ], [ 276, "dart", "var x = \"${{'key': 1}.length}\";", "${{'key': 1}.length}", "subst" ], [ 277, "makefile", "VALUE := $(subst x,y,$(shell printf x))", "$(subst x,y,$(shell printf x))", "variable" ], [ 278, "lua", "local s = [=[ text ]==] still text ]=]\n--[=[ comment ]==] still comment ]=]", "still text", "string" ], [ 279, "xml", '<!DOCTYPE r [<!ENTITY foo2 "ok">]><r>&foo2;</r>', "&foo2;", "symbol" ], [ 280, "elixir", 'value = "#{%{a: 1}}"', "#{%{a: 1}}", "subst" ], [ 281, "erlang", "N = 36#helloworld.", "36#helloworld", "number" ], [ 283, "fsharp", 'let text = $"{ { X = 1 } }"', "{ { X = 1 } }", "subst" ], [ 285, "groovy", 'def s = "Hello ${name.toUpperCase()}"', "${name.toUpperCase()}", "subst" ], [ 286, "perl", 'my $s = "items: @{[ map { $_ * 2 } @items ]}";', "@{[ map { $_ * 2 } @items ]}", "subst" ], [ 287, "r", "x <- 1e+3", "1e+3", "number" ], [ 288, "ruby", 'text = "value: #{ { a: 1 } }"', "#{ { a: 1 } }", "subst" ], [ 289, "swift", 'let raw = ####"value \\####(42)"####', '####"value \\####(42)"####', "string" ], [ 290, "scala", 'val text = s"value ${ { val n = 1; n } }"', "${ { val n = 1; n } }", "subst" ], [ 291, "scss", '$name: "world"; .message { content: "Hello, #{$name}"; }', "#{$name}", "subst" ] ].map((([issue, language, code, marker, scope]) => ({
    issue: issue,
    language: language,
    code: code,
    marker: marker,
    scope: scope
})));

const slash = String.fromCharCode(92);

const controls = [ [ 269, "kotlin", "/* " + "/* ".repeat(8) + "inside" + " */".repeat(8) + " still outer */ val outside=1", "still outer", "comment" ], [ 269, "kotlin", "/* comment */ val outside=1", "val outside", "comment", true ], [ 270, "bash", "echo $(date)", "$(date)", "subst" ], [ 270, "bash", `echo $(printf '%s' ')')`, `$(printf '%s' ')')`, "subst" ], [ 271, "vim", 'let s = "actual string"', '"actual string"', "string" ], [ 272, "csharp", 'var s = $"{new { Text = "}" }.Text}";', '{new { Text = "}" }.Text}', "subst" ], [ 272, "csharp", 'var s = $"{{literal}}";', "literal", "subst", true ], [ 273, "graphql", 'description: """value ' + slash + '""" still inside"""', "still inside", "string" ], [ 273, "graphql", 'x: "simple"', '"simple"', "string" ], [ 274, "go", "func f(n int) { return }", "(n int)", "params" ], [ 275, "cmake", "set(X [=[ text ]==] still text ]=])", "still text", "string" ], [ 275, "cmake", "#[[ comment ]]\nmessage(OK)", "message(OK)", "comment", true ], [ 278, "lua", "--[=[ comment ]==] still comment ]=]", "still comment", "comment" ], [ 278, "lua", "local s=[=[ text [==[ inner ]=] print(1)", "print(1)", "string", true ], [ 278, "lua", "local s=[[valid]]", "valid", "string" ], [ 279, "xml", "<r>&foo2; &name.part-2; &#123; &#xAB; &名2; &2bad;</r>", "&name.part-2;", "symbol" ], [ 279, "xml", "<r>&名2;</r>", "&名2;", "symbol" ], [ 279, "xml", "<r>&2bad;</r>", "&2bad;", "symbol", true ], [ 280, "elixir", "value = ~s(#{%{a: 1}})", "#{%{a: 1}}", "subst" ], [ 281, "erlang", "A = 16#ff_ff.", "16#ff_ff", "number" ], [ 281, "erlang", "A = 2#2.", "2#2", "number", true ], [ 285, "groovy", 'def s = "Hello $user.name"', "$user.name", "variable" ], [ 285, "groovy", "def s = 'Hello ${name}'", "${name}", "subst", true ], [ 285, "groovy", "def s = $/Hello ${name}/$", "${name}", "subst" ], [ 287, "r", "x <- 1.25", "1.25", "number" ], [ 287, "r", "x = 1.25", "1.25", "number" ], [ 288, "ruby", "text = %Q{value: #{ { a: 1 } }}", "#{ { a: 1 } }", "subst" ], [ 289, "swift", 'let s = ####"value ' + slash + '####(42)"####', slash + "####(42)", "subst" ], [ 289, "swift", "let r = ########/foo/########", "########/foo/########", "regexp" ], [ 289, "swift", "let s = " + "#".repeat(40) + '"wide"' + "#".repeat(40), "#".repeat(40) + '"wide"' + "#".repeat(40), "string" ], [ 289, "swift", 'let s = ####"value ' + slash + '###(42)"####', slash + "###(42)", "subst", true ], [ 289, "swift", 'let s = ###"value ' + slash + '###(42)"###', slash + "###(42)", "subst" ], [ 290, "scala", 'val s = s"${ List(1).map(n => { n + 1 }) }"', "${ List(1).map(n => { n + 1 }) }", "subst" ], [ 291, "scss", ".x { content: '#{1 + 1}'; }", "#{1 + 1}", "subst" ] ];

controls.push([ 289, "swift", 'let s = ####"outer ' + slash + '####(#####"inner ' + slash + '#####(42)"#####) tail"####; let outside=1', "let outside", "string", true ]);

cases.push(...controls.map((([issue, language, code, marker, scope, exclude = false]) => ({
    issue: issue,
    language: language,
    code: code,
    marker: marker,
    scope: scope,
    exclude: exclude
}))));

function spans(result) {
    const items = [];
    let at = 0;
    function visit(node, scopes = []) {
        if (typeof node === "string") {
            items.push({
                start: at,
                end: at + node.length,
                text: node,
                scopes: scopes
            });
            at += node.length;
            return;
        }
        const next = node.scope ? [ ...scopes, node.scope ] : scopes;
        for (const c of node.children || []) visit(c, next);
    }
    visit(result._emitter.rootNode);
    return items;
}

function covered(result, code, marker, scope) {
    const start = code.indexOf(marker);
    if (start < 0) throw Error("Missing fixture marker");
    const end = start + marker.length, parts = spans(result).filter((s => s.end > start && s.start < end));
    return parts.length > 0 && parts.every((s => s.scopes.includes(scope)));
}

module.exports = {
    cases: cases,
    spans: spans,
    covered: covered
};
