// Monarch syntax highlighting for the onisin-view language.
export default {
    keywords: [
        'accordion','as','auto_refresh','avatar','badge','bind','bold','button','card','check','color','cols','column','combobox','confirm','currency','date','daterange','datetime','default','delete','divider','email','exit','expand','false','file','focus','format','full','gap','grid','heading','href','icon','italic','item','json','label','lg','link','long','m','max','mb','md','medium','min','ml','modal','mono','mr','mt','multiselect','mx','my','new','number','on','on_edit','on_new','on_select','open','over','p','password','pb','percent','pl','placeholder','plain','pr','progress','pt','px','py','radio','rangeslider','rating','readonly','refresh','replace','richtext','row','save','scroll','section','select','sep','short','size','slider','sm','stack','step','subheading','switch','tab','table','tabs','tags','text','textarea','time','toolbar','true','view','width','window','xl','xs'
    ],
    operators: [
        ',','->','.',':','='
    ],
    symbols: /\(|\)|,|->|\.|:|=|\{|\}/,

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
