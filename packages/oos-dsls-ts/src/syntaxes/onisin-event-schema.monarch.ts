// Monarch syntax highlighting for the onisin-event-schema language.
export default {
    keywords: [
        'EventType','boolean','date','datetime','number','optional','required','string'
    ],
    operators: [
        ':'
    ],
    symbols: /:|\{|\}/,

    tokenizer: {
        initial: [
            { regex: /[_a-zA-Z][a-zA-Z0-9_]*/, action: { cases: { '@keywords': {"token":"keyword"}, '@default': {"token":"ID"} }} },
            { regex: /"(\\.|[^"\\])*"/, action: {"token":"string"} },
            { include: '@whitespace' },
            { regex: /@symbols/, action: { cases: { '@operators': {"token":"operator"}, '@default': {"token":""} }} },
        ],
        whitespace: [
            { regex: /\s+/, action: {"token":"white"} },
            { regex: /\/\*/, action: {"token":"comment","next":"@comment"} },
            { regex: /\/\/[^\n\r]*/, action: {"token":"comment"} },
        ],
        comment: [
            { regex: /[^/\*]+/, action: {"token":"comment"} },
            { regex: /\*\//, action: {"token":"comment","next":"@pop"} },
            { regex: /[/\*]/, action: {"token":"comment"} },
        ],
    }
};
