// Monarch syntax highlighting for the onisin-domain language.
export default {
    keywords: [
        'ai','aliases','belongs_to','bind','bool','date','datetime','delete','domain','eq','example','field','filterable','float','from','ge','gt','has_many','has_one','in','int','le','like','lt','meta','ne','options','order_by','permission','read','readonly','relation','string','text','via','write'
    ],
    operators: [
        ',','->',':','=','@'
    ],
    symbols: /,|->|:|=|@|\[|\]|\{|\}/,

    tokenizer: {
        initial: [
            { regex: /[_a-zA-Z][\w_]*/, action: { cases: { '@keywords': {"token":"keyword"}, '@default': {"token":"ID"} }} },
            { regex: /-?[0-9]+(\.[0-9]+)?/, action: {"token":"number"} },
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
