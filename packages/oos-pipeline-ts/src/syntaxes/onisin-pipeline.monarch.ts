// Monarch syntax highlighting for the onisin-pipeline language.
export default {
    keywords: [
        '_and','_eq','_gt','_gte','_ilike','_in','_like','_lt','_lte','_neq','_not','_or','aggregate','context','db','docs','dsn=','editor','embedding','file','from','input','limit','llm','mass','mode','nats','out','per-row','pipeline','prompt','query','semantic','source','step','subject','system','uri=','where'
    ],
    operators: [
        ':'
    ],
    symbols: /:|\{|\}/,

    tokenizer: {
        initial: [
            { regex: /[_a-zA-Z][\w-]*/, action: { cases: { '@keywords': {"token":"keyword"}, '@default': {"token":"ID"} }} },
            { regex: /[0-9]+/, action: {"token":"number"} },
            { regex: /-?[0-9]+(\.[0-9]+)?/, action: {"token":"number"} },
            { regex: /"[^"]*"|'[^']*'/, action: {"token":"string"} },
            { include: '@whitespace' },
            { regex: /@symbols/, action: { cases: { '@operators': {"token":"operator"}, '@default': {"token":""} }} },
        ],
        whitespace: [
            { regex: /\s+/, action: {"token":"white"} },
            { regex: /\/\/[^\n\r]*/, action: {"token":"comment"} },
            { regex: /\/\*/, action: {"token":"comment","next":"@comment"} },
        ],
        comment: [
            { regex: /[^/\*]+/, action: {"token":"comment"} },
            { regex: /\*\//, action: {"token":"comment","next":"@pop"} },
            { regex: /[/\*]/, action: {"token":"comment"} },
        ],
    }
};
