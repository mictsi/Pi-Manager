const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
const source=fs.readFileSync(path.join(__dirname,'../assets/app.js'),'utf8');
function section(start,end){
    const first=source.indexOf(start),last=source.indexOf(end,first);
    assert(first>=0 && last>first,`Missing source section: ${start}`);
    return source.slice(first,last);
}
function deferred(){let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};}

for(const type of ['line','bar','doughnut']){
    test(`embedded Chart.js ${type} supports rendering and live data updates`,()=>{
        const Chart=require('../assets/chart.umd.min.js');
        const canvas={width:600,height:300,getContext(){return context;}};
        const context=new Proxy({canvas,measureText(value){return {width:String(value).length*6};}}, {
            get(target,key){return key in target ? target[key] : ()=>{};}
        });
        const chart=new Chart(canvas,{type,platform:Chart.BasicPlatform,
            data:{labels:['a','b'],datasets:[{data:[2,3]}]},options:{responsive:false,animation:false}});
        try {
            assert.equal(chart.getDatasetMeta(0).data.length,2);
            chart.data.labels=['a','b','c'];chart.data.datasets[0].data=[4,1,7];
            chart.update('none');
            assert.equal(chart.getDatasetMeta(0).data.length,3);
        }finally{chart.destroy();}
    });
}

test('a post-mutation refresh waits for a fresh poll',async()=>{
    const pending=deferred(),controls=new Map();let calls=0,blocking=true,displayed;
    const context=vm.createContext({
        preference:(_key,fallback)=>fallback,savePreference(){},
        $:selector=>{if(!controls.has(selector))controls.set(selector,{setAttribute(){}});return controls.get(selector);},
        document:{hidden:false},setTimeout:()=>0,clearTimeout(){},t:key=>key,localeCode:()=> 'en',humanError:String,Date,Promise
    });
    vm.runInContext(section('async function startPolling(', 'document.addEventListener("visibilitychange"'),context);
    const page={active:true};
    const initial=context.startPolling(page,{},'dashboard',async()=>{
        const snapshot=blocking;
        if(++calls===1)await pending.promise;
        displayed=snapshot;
    });
    blocking=false;
    const fresh=page.refresh(true),alsoFresh=page.refresh(true);
    pending.resolve();await initial;await fresh;await alsoFresh;
    assert.equal(calls,2);assert.equal(displayed,false);
});

test('older record GETs cannot undo a completed mutation refresh',async()=>{
    const older=deferred(),newer=deferred();let calls=0;
    const context=vm.createContext({
        page:{active:true},kind:'domains',RECORD_PATHS:{domains:'/domains'},loadGeneration:0,
        byServer:new Map(),loaded:new Set(),state:{servers:[{id:'1',connected:true}]},
        api:()=>++calls===1 ? older.promise : newer.promise,
        recordItems:(_kind,data)=>data.domains,rebuild(){},draw(){},$:()=>({}),container:{},t:key=>key,humanError:String
    });
    vm.runInContext(section('    async function loadServers(servers)', '    async function runChanges('),context);
    const server={id:'1',connected:true},before=context.loadServers([server]),after=context.loadServers([server]);
    newer.resolve({domains:[{domain:'example.test',enabled:false,comment:'new'}]});await after;
    older.resolve({domains:[{domain:'example.test',enabled:true,comment:'old'}]});await before;
    assert.equal(context.byServer.get('1')[0].enabled,false);
    assert.equal(context.byServer.get('1')[0].comment,'new');
});

test('saving Settings uses the backend snapshot and disables the server form',async()=>{
    const gate=deferred(),control={disabled:false,textContent:''},formFields=[{}, {}, {}];let persisted;
    const context=vm.createContext({
        rows:[{id:'1',url:'http://one.test',role:'main',password:''},{id:'2',url:'http://two.test',role:'secondary',password:''}],
        restoreState:{running:false},saving:false,state:{servers:[],settingsSaving:false},page:{active:false},container:{},
        readRows(){},$:()=>control,$$:()=>formFields,
        nativeSaveServers:async rows=>{
            persisted=structuredClone(rows);await gate.promise;
            return {config:{servers:persisted.map(server=>({...server,url:server.url+'/api'}))},revision:1};
        },desktopConfig:{},desktopRevision:0,renderPills(){},switchTab(){},toast(){},showError(error){throw error;},t:key=>key
    });
    vm.runInContext(section('function acceptConfiguration(', 'async function api('),context);
    const handler=section('$("#desktopSettings",container).onsubmit=async event=>{','    const appearance=');
    vm.runInContext(handler.replace('$("#desktopSettings",container).onsubmit=','submit='),context);
    const pending=context.submit({preventDefault(){}});
    assert(formFields.every(field=>field.disabled));assert(context.state.settingsSaving);
    vm.runInContext('rows=rows.filter(server=>server.id!=="2")',context);
    gate.resolve();await pending;
    assert.equal(persisted.length,2);assert.equal(context.state.servers.length,2);
    assert.equal(context.state.servers[0].url,'http://one.test/api');
    assert.equal(context.state.servers[0].revision,1);assert(!context.state.settingsSaving);
});

test('Local DNS keeps IPv4, IPv6 and multiple IPv4 answers',()=>{
    const context=vm.createContext({kind:'hosts',selected:new Set(),byServer:new Map(),merged:new Map(),URL});
    vm.runInContext(section('function recordKey(', 'function recordDeletePath('),context);
    vm.runInContext(section('    function rebuild()', '    function rowHTML('),context);
    const values=['192.0.2.1 nas.home','192.0.2.2 nas.home','2001:db8::1 nas.home'];
    context.byServer.set('1',context.recordItems('hosts',{config:{dns:{hosts:values}}}));
    context.byServer.set('2',context.recordItems('hosts',{config:{dns:{hosts:[values[1],values[2]]}}}));
    context.rebuild();
    assert.equal(context.merged.size,3);
    const rows=[...context.merged.values()];
    assert.equal(rows.filter(row=>row.byServer.has('2')).length,2);
    assert(rows.find(row=>row.item.ip==='192.0.2.2').byServer.has('2'));
    assert(!rows.find(row=>row.item.ip==='192.0.2.1').byServer.has('2'));
});

test('polling reconciles a slow query after its status and reply change',async()=>{
    let now=1000,dataset=[{id:1,time:990,status:'IN_PROGRESS',reply:{type:'UNKNOWN'}},{id:2,time:999,status:'CACHE',reply:{type:'IP'}}];
    const context=vm.createContext({
        page:{active:true,pollGeneration:0},model:{range:6},resetRequested:true,limited:false,lastReconciled:0,
        rows:new Map(),cursors:new Map(),latest:new Map(),clientNames:new Map(),displayedAt:0,
        state:{servers:[{id:'1',connected:true}]},draw(){},humanError:String,
        api:async(_server,request)=>request==='/clients' ? {clients:[]} : {queries:dataset.filter(query=>query.time>=Number(new URL('http://test'+request).searchParams.get('from')))},
        Date:class extends Date{static now(){return now*1000;}}
    });
    vm.runInContext(section('function queryOutcome(', 'function queryOutcomeLabel('),context);
    vm.runInContext(section('    function merge(server, result)', '    tbody.onclick=async event=>'),context);
    const start=source.indexOf('    await startPolling(page,container,"queries",async()=>{');
    const end=source.indexOf('\n    });\n}',start);
    vm.runInContext(source.slice(start,end).replace('    await startPolling(page,container,"queries",async()=>{','pollTick=async()=>{')+'\n};',context);
    await context.pollTick();now=1040;
    dataset=[{id:1,time:990,status:'FORWARDED',reply:{type:'IP',time:10}},{id:3,time:1039,status:'CACHE',reply:{type:'IP'}}];
    await context.pollTick();
    assert.equal(context.rows.get('1:1').status,'FORWARDED');
    assert.equal(context.rows.get('1:1').reply.time,10);
});

test('per-host Check again replaces the displayed versions and update indicator',async()=>{
    const button={disabled:false,textContent:''},server={id:'1',url:'http://one.test/api',revision:2,label:'Main'};
    let options,calls=0,rendered=0;
    const context=vm.createContext({
        hostHealth:new Map([['1',{versions:{components:[{component:'core',local:'v6.1',latest:'v6.2',status:'update'}]}}]]),
        inspector:null,textHTML:key=>key,t:key=>key,escapeHtml:String,versionLabel:String,versionStatusText:String,fmtDate:String,
        showInspector:(_title,_body,actions)=>{options={dialog:{},actions};context.inspector=options;return options;},
        $:()=>button,setHTML(element,html){element.innerHTML=html;},serverById:()=>server,renderPills:()=>rendered++,showError(error){throw error;},
        nativeCheckVersions:async(id,url,revision)=>{assert.equal(id,'1');assert.equal(url,server.url);assert.equal(revision,2);calls++;return {components:[{component:'core',local:'v6.2',latest:'v6.2',status:'current'}]};}
    });
    vm.runInContext(section('function serverVersionsHTML(', 'function renderPills('),context);
    context.showServerVersions(server);
    assert(options.actions.includes('data-check-versions'));
    await button.onclick({currentTarget:button});
    assert.equal(calls,1);assert.equal(rendered,1);
    assert.equal(context.hostHealth.get('1').versions.components[0].status,'current');
    assert(!button.disabled);
});
