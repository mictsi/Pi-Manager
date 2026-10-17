package main

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// Optional native integration test. Run under a real display or xvfb-run with
// PI_MANAGER_DESKTOP_TEST=1 and the system WebKitGTK runtime installed.
func TestNativeDesktop(t *testing.T) {
	if os.Getenv("PI_MANAGER_DESKTOP_TEST") != "1" {
		t.Skip("set PI_MANAGER_DESKTOP_TEST=1 to test the native window")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	a := desktopTestApp(t)
	cfg := a.config.Get()
	for i := range cfg.Servers {
		cfg.Servers[i].URL = ""
	}
	if err := a.config.SetServers(cfg.Servers); err != nil {
		t.Fatal(err)
	}
	w, err := newDesktop(a)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Destroy()
	if err := setWindowIcon(w.Window()); err != nil {
		t.Fatal("native window icon:", err)
	}
	if err = w.Bind("smokeResize", func(width, height int) { w.Dispatch(func() { w.SetSize(width, height, 0) }) }); err != nil {
		t.Fatal(err)
	}
	w.SetSize(2400, 1000, 0)
	result := make(chan string, 1)
	if err = w.Bind("smokeReport", func(message string) {
		select {
		case result <- message:
		default:
		}
		w.Terminate()
	}); err != nil {
		t.Fatal(err)
	}
	w.Init(`
        window.addEventListener("load", async () => {
            const errors = [];
            window.addEventListener("error", e => errors.push(e.message));
            window.addEventListener("unhandledrejection", e => errors.push(String(e.reason)));
            const assert = (condition, message) => { if (!condition) throw new Error(message); };
            const wait = async test => {
                for (let n=0; n<400; n++) { if (test()) return; await new Promise(r=>setTimeout(r,25)); }
                throw new Error("Timed out waiting for dashboard state");
            };
            try {
                await wait(() => typeof state !== "undefined" && state.ready && document.getElementById("desktopSettings"));
                assert(document.getElementById("tabContent").getBoundingClientRect().width >= innerWidth-2, "Window width is constrained");
                document.querySelector('[data-server="1"] [data-url]').value="http://one.test/api";
                document.querySelector('[data-server="2"] [data-url]').value="http://two.test/api";
                document.querySelector('[data-server="1"] [data-password]').value="saved-one";
                document.querySelector('[data-server="2"] [data-password]').value="saved-two";
                document.getElementById("desktopSettings").requestSubmit();
                await wait(() => state.servers.every(s=>s.connected && !s.checking));
                await setLanguage("en");
                try {
                    await nativeRequest("1", "invalid-path", "GET", "", false);
                    throw new Error("Invalid request was accepted");
                } catch (error) { assert(error.message.includes("API path"), "Native errors are not Error objects"); }
                const trayStatus=await nativeTrayStatus();
                if(!trayStatus.available) {
                    assert(document.getElementById("minimizeTray").disabled,"Unavailable tray must not hide the window");
                    try {await nativeMinimizeToTray();throw new Error("Unavailable tray was accepted")}catch(error){assert(error.message.includes("system tray unavailable"),"Missing tray failure reason")}
                }
                for (const values of Object.values(MESSAGES)) assert(values.length===4 && values.every(Boolean), "Incomplete translation");
                savePreference("dashboard_details",false);
                for (const language of ["en","el","sv","da","en"]) {
                    await setLanguage(language);
                    for (const tab of ["dashboard","queries","domains","hosts","clients","lists","gravity","drift","settings"]) {
                        await switchTab(tab);
                        assert(document.documentElement.lang===language,"Wrong document language");
                        if(["domains","hosts","clients","lists"].includes(tab))assert(document.querySelector("[data-sync-secondaries]")?.textContent===t("syncSecondaries"),"Missing translated resource sync action");
                        if(tab === "domains")assert(document.querySelector(".wildcard-option label").textContent===t("addDomainWildcard") && document.getElementById("domainWildcardHelp").textContent===t("domainWildcardHelp"),"Missing translated wildcard option");
                        else assert(!document.querySelector("[data-domain-wildcard]"),"Wildcard option appeared outside Domains");
                        if(tab === "gravity")assert(document.querySelector("[data-gravity-all]")?.textContent===t("updateGravityAll"),"Missing translated all-server Gravity action");
                        if(tab === "queries") {
                            assert(document.querySelector('[data-sort="queryResult"]').textContent===t("queryResult"),"Missing translated query outcome column");
                            assert(document.querySelector(".query-result svg")?.getAttribute("aria-hidden")==="true","Query status icon lacks accessible text handling");
                            assert(document.querySelector('[data-query-action="allow"]').textContent===t("allow") && document.querySelector('[data-query-action="deny"]').textContent===t("deny"),"Missing translated query actions");
                        }
                        assert(document.title==="Pi-Manager" && document.querySelector("header h1").textContent.startsWith("Pi-Manager"),"App branding changed during translation");
                        if(tab!=="dashboard")assert(document.querySelector(".view-heading") && document.querySelector(".view-icon svg"),"Missing workspace styling: "+tab);
                        assert(document.querySelector("#tabs [data-tab=queries]").textContent===t("queries"),"Tab was not translated");
                        const content = document.getElementById("tabContent").innerText;
                        if (language==="en") assert(!/Forespørgsler|Tilføj|Indstillinger|Adgangskode|slået fra/.test(content),"Danish UI remains in English mode");
                        assert(!/Missing UI message/.test(content),"Missing UI message");
                        if (["domains","hosts","lists"].includes(tab)) {
                            const main=document.getElementById("tabContent");
                            main.style.width="850px";
                            for (const group of document.querySelectorAll(".datatable .record-actions")) {
                                const buttons=[...group.querySelectorAll("button")];
                                const first=buttons[0].getBoundingClientRect();
                                assert(buttons.every(b=>Math.abs(b.getBoundingClientRect().top-first.top)<1),"Table actions wrapped: "+tab+"/"+language);
                                const status=group.previousElementSibling?.getBoundingClientRect();
                                if (status) assert(Math.abs((status.top+status.height/2)-(first.top+first.height/2))<3,"Status and actions are misaligned: "+tab);
                            }
                            const bulk=[...document.querySelectorAll(".bulk-toolbar button")].map(b=>b.getBoundingClientRect().top);
                            assert(bulk.every(y=>Math.abs(y-bulk[0])<1),"Bulk buttons wrapped: "+tab+"/"+language);
                            main.style.width="";
                        }
                    }
                }
                await switchTab("dashboard");
                const details=document.querySelector("[data-fold=details]"),top=document.querySelector("[data-fold=top]");
                assert(details.open && !top.open,"Details must start expanded and top panels collapsed");
                assert(document.querySelectorAll(".metric-tile").length===5 && document.querySelector(".metric-ring") && document.querySelector(".metric-sparkline polyline"),"Visual dashboard metrics are missing");
                details.open=true;
                await wait(()=>!state.page.busy && Chart.getChart(document.querySelector("canvas")));
                const canvas=document.querySelector("canvas"),chart=Chart.getChart(canvas);
                await state.page.refresh();
                assert(document.querySelector("canvas")===canvas && Chart.getChart(canvas)===chart,"Polling recreated charts");
                details.open=false;
                const refresh=document.querySelector(".refresh-interval");
                assert(refresh.value==="2000","Default dashboard refresh is not two seconds");
                assert(["500","1000","2000","10000","30000","60000"].every(v=>[...refresh.options].some(o=>o.value===v)),"Missing refresh intervals");
                const before=document.getElementById("summary1").textContent;
                refresh.value="500";refresh.dispatchEvent(new Event("change"));
                await wait(()=>document.getElementById("summary1").textContent!==before);
                assert(!details.open,"Polling reopened collapsed panels");
                assert(!document.querySelector("[data-logout],[data-settings-logout]"),"Logout remains outside Settings");
                assert(document.getElementById("serverPills").parentElement.classList.contains("topbar-row"),"Server status is not in the top row");
                const combined=combineDashboard([
                    {summary:{queries:{total:100,blocked:10}},history:{history:[{timestamp:1200,total:10,blocked:1},{timestamp:1800,total:20,blocked:2}]},queryTypes:{types:{A:3}}},
                    {summary:{queries:{total:300,blocked:90}},history:{history:[{timestamp:1800,total:30,blocked:3},{timestamp:1200,total:40,blocked:4}]},queryTypes:{types:{A:7,AAAA:2}}}
                ]);
                assert(combined.summary.queries.total===400 && combined.summary.queries.blocked===100,"Combined totals are incorrect");
                assert(combined.history.history[0].total===50 && combined.history.history[1].total===50,"Histories were merged by position instead of timestamp");
                assert(combined.queryTypes.types.A===10 && combined.queryTypes.types.AAAA===2,"Query types were not aggregated");
                assert(document.querySelector(".metric-blocklistDomains .big-num").textContent===fmtNum(12345),"Blocklist domains must use main, not the sum");
                assert(document.querySelector(".blocklist-warning")?.textContent.includes("Two") && document.querySelector(".blocklist-warning").textContent.includes(fmtNum(12340)),"Secondary blocklist-count warning is missing");
                const countServers=[{id:"1",label:"Main",role:"main",url:"one",connected:true},{id:"2",label:"Secondary",role:"secondary",url:"two",connected:true}];
                const countData=new Map([["1",{summary:{gravity:{domains_being_blocked:100}}}],["2",{summary:{gravity:{domains_being_blocked:80}}}]]);
                const countDrift=blocklistOverview(countData,countServers);
                assert(countDrift.count===100 && countDrift.drift.length===1 && countDrift.drift[0].count===80,"Main blocklist drift was not detected");
                countData.delete("1");assert(blocklistOverview(countData,countServers).count===null,"Unavailable main fell back to secondary's count");
                countData.set("1",{summary:{gravity:{domains_being_blocked:100}}});countServers[1].connected=false;
                assert(blocklistOverview(countData,countServers).unknown.length===1,"Unavailable secondary was treated as matching");
                const host2=document.querySelector('[data-dashboard-host="2"]');host2.checked=false;host2.dispatchEvent(new Event("change",{bubbles:true}));
                await wait(()=>!state.page.busy);
                const totalText=()=>document.querySelector("#summaryCombined .big-num").textContent;
                assert(document.getElementById("summary2").hidden,"Filtered host card is still visible");
                assert(document.querySelector(".blocklist-warning")?.textContent.includes("Two"),"Filtering a secondary hid its blocklist drift warning");
                assert(totalText()===document.querySelector("#summary1 .big-num").textContent,"Host filter did not affect combined totals");
                const host1=document.querySelector('[data-dashboard-host="1"]');host1.checked=false;host2.checked=true;host2.dispatchEvent(new Event("change",{bubbles:true}));
                await wait(()=>!state.page.busy);
                assert(document.querySelector(".metric-blocklistDomains .big-num").textContent===fmtNum(12345),"Filtering main out removed the main-only blocklist count");
                const allHosts=document.querySelector("[data-all-hosts]");allHosts.checked=false;allHosts.dispatchEvent(new Event("change",{bubbles:true}));
                await wait(()=>!state.page.busy);assert(totalText()==="0","Deselected hosts remain in totals");
                allHosts.checked=true;allHosts.dispatchEvent(new Event("change",{bubbles:true}));await wait(()=>!state.page.busy);
                await switchTab("drift");
                document.querySelector("[data-preview-sync]").click();
                await wait(()=>!document.querySelector("[data-preview-sync]").disabled);
                assert(document.querySelector("[data-apply-sync]").disabled,"Sync must wait for explicit row selection");
                const dnsChoice=document.querySelector("[data-drift-select]:not(:disabled)");dnsChoice.click();
                assert(!document.querySelector("[data-apply-sync]").disabled,"Selecting DNS drift did not enable sync");
                document.querySelector("[data-drift-filter]").value="no-match";document.querySelector("[data-drift-filter]").dispatchEvent(new Event("input"));
                assert(document.querySelector("[data-drift-selection]").textContent.includes(t("hiddenSelection",{n:1})),"Hidden selections are not disclosed");
                document.querySelector("[data-drift-filter]").value="";document.querySelector("[data-drift-filter]").dispatchEvent(new Event("input"));
                assert(document.querySelector("[data-drift-select]").checked,"Filtering lost drift selection");
                document.querySelector("[data-clear-drift]").click();assert(document.querySelector("[data-apply-sync]").disabled,"Clear selection did not disable sync");
                document.querySelector("[data-select-drift]").click();
                assert(document.querySelectorAll("tbody tr[data-key]").length===1,"Unexpected drift rows");
                window.confirm=()=>true;document.querySelector("[data-apply-sync]").click();
                await wait(()=>document.querySelector("[data-sync-status]").textContent===t("syncDone",{applied:1,failed:0}));
                document.querySelector("[data-preview-sync]").click();await wait(()=>!document.querySelector("[data-preview-sync]").disabled);
                assert(document.querySelector("[data-sync-status]").textContent===t("syncSummary",{changes:0,conflicts:0}),"Sync did not resolve DNS drift");
                assert(document.querySelector("[data-apply-sync]").disabled,"Empty sync should not be applied");
                await switchTab("queries");
                const queryFilter=document.querySelector("[data-filter]"),queryTable=document.querySelector("table");
                queryFilter.value="example.com";queryFilter.dispatchEvent(new Event("input"));
                const queryRate=document.querySelector(".refresh-interval");
                assert(queryRate.value==="2000","Query refresh should be independent");
                await state.page.refresh();
                assert(document.querySelector("[data-filter]")===queryFilter && queryFilter.value==="example.com" && document.querySelector("table")===queryTable,"Query refresh reset controls");
                assert(document.querySelectorAll("tbody tr[data-key]").length===4,"Polling skipped the next query API page");
                for(const status of ["GRAVITY","DENYLIST","REGEX","GRAVITY_CNAME","DENYLIST_CNAME","REGEX_CNAME","EXTERNAL_BLOCKED_IP","EXTERNAL_BLOCKED_NULL","EXTERNAL_BLOCKED_NXRA","EXTERNAL_BLOCKED_EDE15","DBBUSY","SPECIAL_DOMAIN","QUERY_EXTERNAL_BLOCKED_EDE15"])assert(queryOutcome(status)==="blocked","Blocked query status misclassified: "+status);
                for(const status of ["FORWARDED","CACHE","CACHE_STALE","RETRIED","RETRIED_DNSSEC"])assert(queryOutcome(status)==="allowed","Allowed query status misclassified: "+status);
                assert(queryOutcome("UNKNOWN")==="unknown" && queryOutcome("FUTURE_STATUS")==="unknown" && queryOutcome("IN_PROGRESS")==="pending","Unknown or pending query was shown as allowed");
                const queryRow=key=>[...document.querySelectorAll("tbody tr[data-key]")].find(row=>row.dataset.key===key);
                const allowedQuery=queryRow("1:1"),blockedQuery=queryRow("1:2");
                assert(allowedQuery.dataset.queryOutcome==="allowed" && allowedQuery.querySelector(".query-result").textContent===t("allowed"),"Missing allowed query status");
                assert(blockedQuery.dataset.queryOutcome==="blocked" && blockedQuery.querySelector(".query-result").textContent===t("blocked"),"Missing blocked query status");
                assert(queryRow("2:2").dataset.queryOutcome==="blocked","Database-busy block was shown as allowed");
                const originalTheme=document.documentElement.dataset.theme;
                for(const theme of ["light","dark"]) {
                    document.documentElement.dataset.theme=theme;
                    assert(getComputedStyle(allowedQuery.cells[0]).backgroundColor!==getComputedStyle(blockedQuery.cells[0]).backgroundColor,"Allowed and blocked rows have identical colors in "+theme+" mode");
                }
                document.documentElement.dataset.theme=originalTheme;
                const originalQueryRequest=window.nativeRequest;let releaseQueryAction;
                const queryActionGate=new Promise(resolve=>releaseQueryAction=resolve);
                window.nativeRequest=async(...args)=>{if(args[1].startsWith("/domains/") && args[2]==="POST")await queryActionGate;return originalQueryRequest(...args)};
                allowedQuery.querySelector('[data-query-action="allow"]').click();
                await wait(()=>queryRow("1:1").querySelector('[data-query-action="allow"]').disabled);
                await state.page.refresh();
                assert(queryRow("1:1")===allowedQuery && queryRow("1:1").querySelector('[data-query-action="deny"]').disabled,"Polling lost query action interlock or replaced the row");
                releaseQueryAction();await wait(()=>!queryRow("1:1").querySelector('[data-query-action="allow"]').disabled);
                window.nativeRequest=originalQueryRequest;
                const queryDomains=async id=>JSON.parse((await nativeRequest(id,"/domains","GET","",false)).body).domains;
                let queryRules=await queryDomains("1");
                assert(queryRules.some(item=>item.domain==="example.com" && item.type==="allow" && item.kind==="exact" && item.enabled && item.groups[0]===0),"Query allow did not create an exact allow rule");
                assert(!(await queryDomains("2")).some(item=>item.domain==="example.com"),"Query allow modified a different server");
                await api(serverById("1"),"/domains/allow/exact/example.com",{method:"PUT",body:{comment:"existing query rule",groups:[3],enabled:false}});
                queryRow("1:1").querySelector('[data-query-action="allow"]').click();await wait(()=>!queryRow("1:1").querySelector('[data-query-action="allow"]').disabled);
                queryRules=await queryDomains("1");
                assert(queryRules.filter(item=>item.domain==="example.com").length===1 && queryRules.find(item=>item.domain==="example.com").enabled,"Repeated query allow duplicated or failed to enable the existing rule");
                queryRow("1:1").querySelector('[data-query-action="deny"]').click();await wait(()=>!queryRow("1:1").querySelector('[data-query-action="deny"]').disabled);
                queryRules=await queryDomains("1");
                const deniedQueryRule=queryRules.find(item=>item.domain==="example.com");
                assert(deniedQueryRule.type==="deny" && deniedQueryRule.kind==="exact" && deniedQueryRule.comment==="existing query rule" && deniedQueryRule.groups[0]===3 && !queryRules.some(item=>item.domain==="example.com" && item.type==="allow"),"Query deny did not move the exact allow rule or preserve its metadata");
                assert(queryRow("1:1").dataset.queryOutcome==="allowed","Saving a rule rewrote a historical query result");
                await api(serverById("1"),"/domains/allow/exact",{method:"POST",body:{domain:["example.com"],type:"allow",kind:"exact",enabled:true,groups:[0]}});
                queryRow("1:1").querySelector('[data-query-action="deny"]').click();await wait(()=>!queryRow("1:1").querySelector('[data-query-action="deny"]').disabled);
                queryRules=await queryDomains("1");assert(queryRules.filter(item=>item.domain==="example.com").length===1 && queryRules.find(item=>item.domain==="example.com").type==="deny","Query deny retained a conflicting exact allow rule");
                window.nativeRequest=async(...args)=>args[1].startsWith("/domains/") && args[2]==="POST" ? {status:200,body:'{"processed":{"errors":[{"error":"query rule rejected"}]}}'} : originalQueryRequest(...args);
                queryRow("2:2").querySelector('[data-query-action="allow"]').click();await wait(()=>!queryRow("2:2").querySelector('[data-query-action="allow"]').disabled);
                assert(document.getElementById("toastArea").textContent.includes("query rule rejected"),"Per-item query rule rejection was hidden");
                assert(!(await queryDomains("2")).some(item=>item.domain==="older.example.com"),"Rejected query action changed the server");
                window.nativeRequest=async(...args)=>{
                    const result=await originalQueryRequest(...args);
                    if(args[1].startsWith("/queries")) {
                        const body=JSON.parse(result.body);
                        for(const item of body.queries)if(item.id===1)item.status=args[0]==="1" ? "IN_PROGRESS" : "FUTURE_STATUS";
                        result.body=JSON.stringify(body);
                    }
                    return result;
                };
                await state.page.refresh();
                assert(queryRow("1:1").dataset.queryOutcome==="pending" && queryRow("1:1").querySelector(".query-result").textContent===t("queryPending"),"Pending row is not distinguished from allowed");
                assert(queryRow("2:1").dataset.queryOutcome==="unknown" && queryRow("2:1").querySelector(".query-result").textContent===t("queryUnknown"),"Unknown row is not neutral");
                window.nativeRequest=originalQueryRequest;await state.page.refresh();
                assert(document.querySelector("[data-filter]")===queryFilter && queryFilter.value==="example.com" && document.querySelector("table")===queryTable,"Query actions reset the filter or table");
                await api(serverById("1"),"/domains/deny/exact/example.com",{method:"DELETE"});
                await switchTab("domains");
                const domainForm=document.getElementById("recordForm"),domainTable=document.querySelector("table"),domainFilter=document.querySelector("[data-record-filter]");
                const originalRows=[...document.querySelectorAll("tbody tr[data-key]")];
                assert(originalRows.length===2,"Missing mock domains");
                originalRows[0].querySelector('[data-record-server="1"] [data-action=disable]').click();
                await wait(()=>originalRows[0].querySelector('[data-record-server="1"] .badge')?.textContent===t("disabled"));
                assert(document.getElementById("recordForm")===domainForm && document.querySelectorAll("tbody tr[data-key]")[1]===originalRows[1],"Single mutation replaced unrelated nodes");
                const selectAll=document.querySelector("[data-select-all]");selectAll.checked=true;selectAll.dispatchEvent(new Event("change"));
                document.querySelector('[data-bulk="disable"]').click();
                await wait(()=>[...document.querySelectorAll("tbody .badge")].every(b=>b.textContent===t("disabled")));
                domainFilter.value="example";domainFilter.dispatchEvent(new Event("input"));
                document.querySelector("[data-values]").value="new.example\nsecond-new.example";
                domainForm.requestSubmit();
                await wait(()=>document.querySelectorAll("tbody tr[data-key]").length===4);
                assert(document.getElementById("recordForm")===domainForm && document.querySelector("table")===domainTable && domainFilter.value==="example","Add replaced the page or reset its filter");
                assert(document.querySelector("[data-selected]").textContent===t("selected",{n:2}),"Adding reset the selection");
                const domainData=JSON.parse((await nativeRequest("1","/domains","GET","",false)).body);
                const preserved=domainData.domains.find(d=>d.domain==="ads.example");
                assert(preserved.enabled===false && preserved.comment==="keep comment" && preserved.groups[0]===3,"Toggle lost domain metadata");
                document.querySelector("[data-values]").value="reject.example";
                domainForm.requestSubmit();
                await wait(()=>document.getElementById("toastArea").textContent.includes(t("failedChanges",{n:1})));
                const firstPartial=JSON.parse((await nativeRequest("1","/domains","GET","",false)).body);
                const secondPartial=JSON.parse((await nativeRequest("2","/domains","GET","",false)).body);
                assert(firstPartial.domains.some(d=>d.domain==="reject.example") && !secondPartial.domains.some(d=>d.domain==="reject.example"),"Partial failure was not reflected per server");
                const wildcard=document.querySelector("[data-domain-wildcard]"),domainKind=document.querySelector("[data-record-kind]"),domainType=document.querySelector("[data-record-type]"),addDomain=document.querySelector('#recordForm button[type="submit"]');
                assert(!wildcard.checked && !wildcard.disabled,"Wildcard option must start unchecked");
                wildcard.checked=true;
                document.querySelector("[data-values]").value="wildcard.example\n*.another.example";
                document.querySelector("[data-comment]").value="wildcard test";
                domainForm.requestSubmit();await wait(()=>!addDomain.disabled);
                const wildcardPatterns=["(\\.|^)wildcard\\.example$","(\\.|^)another\\.example$"];
                for(const id of ["1","2"]) {
                    const records=JSON.parse((await nativeRequest(id,"/domains","GET","",false)).body).domains;
                    for(const pattern of wildcardPatterns)assert(records.some(d=>d.domain===pattern && d.kind==="regex" && d.type==="deny" && d.comment==="wildcard test" && d.groups[0]===0 && d.enabled),"Wildcard domain was not saved as a regex with metadata on every target");
                    assert(!records.some(d=>d.kind==="exact" && ["wildcard.example","*.another.example"].includes(d.domain)),"Wildcard addition also created an exact entry");
                }
                const wildcardMatcher=new RegExp(wildcardPatterns[0],"i");
                assert(["wildcard.example","ads.wildcard.example","a.b.wildcard.example"].every(value=>wildcardMatcher.test(value)),"Wildcard regex misses the base domain or nested subdomains");
                assert(["notwildcard.example","wildcardXexample","wildcard.example.other"].every(value=>!wildcardMatcher.test(value)),"Wildcard regex matches outside the requested domain");
                domainType.value="allow";document.querySelector("[data-values]").value="*.allowed.example";
                domainForm.requestSubmit();await wait(()=>!addDomain.disabled);
                for(const id of ["1","2"]) {
                    const records=JSON.parse((await nativeRequest(id,"/domains","GET","",false)).body).domains;
                    assert(records.some(d=>d.domain==="(\\.|^)allowed\\.example$" && d.kind==="regex" && d.type==="allow"),"Wildcard allow entry used the wrong API kind or list");
                }
                wildcard.checked=false;document.querySelector("[data-values]").value="plain.example";
                domainForm.requestSubmit();await wait(()=>!addDomain.disabled);
                wildcard.checked=true;domainKind.value="regex";domainKind.dispatchEvent(new Event("change"));
                assert(!wildcard.checked && wildcard.disabled && document.querySelector(".wildcard-option").hidden,"Manual regex mode still enables wildcard conversion");
                const manualPattern="^manual[0-9]+\\.example$";document.querySelector("[data-values]").value=manualPattern;
                domainForm.requestSubmit();await wait(()=>!addDomain.disabled);
                for(const id of ["1","2"]) {
                    const records=JSON.parse((await nativeRequest(id,"/domains","GET","",false)).body).domains;
                    assert(records.some(d=>d.domain==="plain.example" && d.kind==="exact" && d.type==="allow"),"Unchecked wildcard changed normal exact additions");
                    assert(records.some(d=>d.domain===manualPattern && d.kind==="regex" && d.type==="allow"),"Manual regex was rewritten");
                }
                domainKind.value="exact";domainKind.dispatchEvent(new Event("change"));
                assert(!wildcard.checked && !wildcard.disabled && !document.querySelector(".wildcard-option").hidden,"Returning to exact mode did not restore the wildcard option");
                assert(document.getElementById("recordForm")===domainForm && document.querySelector("table")===domainTable && domainFilter.value==="example","Wildcard addition replaced the form or reset the filter");
                await switchTab("lists");
                const listForm=document.getElementById("recordForm"),listFilter=document.querySelector("[data-record-filter]");
                listFilter.value="example.com";listFilter.dispatchEvent(new Event("input"));
                const listAll=document.querySelector("[data-select-all]");listAll.checked=true;listAll.dispatchEvent(new Event("change"));
                document.querySelector('[data-bulk="disable"]').click();
                await wait(()=>[...document.querySelectorAll("tbody .badge")].every(b=>b.textContent===t("disabled")));
                const listData=JSON.parse((await nativeRequest("1","/lists","GET","",false)).body);
                assert(listData.lists.every(l=>l.enabled===false) && listData.lists[0].groups[0]===3 && listData.lists[0].comment==="keep list","List toggle lost metadata");
                document.querySelector("[data-values]").value="https://example.com/new1\nhttps://example.com/new2";
                listForm.requestSubmit();
                await wait(()=>document.querySelectorAll("tbody tr[data-key]").length===4);
                assert(document.getElementById("recordForm")===listForm && listFilter.value==="example.com","List add reset the page");
                await switchTab("hosts");
                window.prompt=()=>"192.168.1.99";
                document.querySelector('[data-record-server="1"] [data-action=edit]').click();
                await wait(()=>document.getElementById("toastArea").textContent.includes(t("restored")));
                const dns=JSON.parse((await nativeRequest("1","/config/dns/hosts","GET","",false)).body);
                assert(dns.config.dns.hosts.includes("192.168.1.3 nas.home"),"DNS rollback did not restore the original value");
                const quickCases=[
                    {kind:"domains",source:{domain:"quick.example",type:"deny",kind:"exact",comment:"from main",groups:[3],enabled:false},extra:{domain:"secondary-only.example",type:"deny",kind:"exact",groups:[0]}},
                    {kind:"hosts",source:{ip:"192.168.1.70",name:"quick.home",raw:"192.168.1.70 quick.home"},extra:{ip:"192.168.1.71",name:"secondary-only.home",raw:"192.168.1.71 secondary-only.home"}},
                    {kind:"clients",source:{client:"192.168.1.80",comment:"from main",groups:[3]},extra:{client:"192.168.1.81",comment:"secondary only",groups:[0]}},
                    {kind:"lists",source:{address:"https://example.com/quick",type:"block",comment:"from main",groups:[3],enabled:false},extra:{address:"https://example.com/secondary-only",type:"block",groups:[0]}}
                ];
                for(const {kind,source,extra} of quickCases) {
                    await sendRecordRequest(serverById("2"),recordCreateRequest(kind,extra));
                    if(kind === "hosts") {
                        const before=recordItems(kind,await api(serverById("2"),RECORD_PATHS[kind])).find(item=>item.name==="nas.home");
                        await api(serverById("2"),recordDeletePath(kind,before),{method:"DELETE"});
                        await api(serverById("2"),"/config/dns/hosts/"+encodeURIComponent("192.168.1.72 nas.home"),{method:"PUT"});
                    }
                    if(kind === "clients")await api(serverById("2"),"/clients/192.168.1.50",{method:"PUT",body:{comment:"secondary drift",groups:[0]}});
                    await switchTab(kind);
                    // Simulate a main-server edit made outside this open view.
                    await sendRecordRequest(serverById("1"),recordCreateRequest(kind,source));
                    const syncButton=document.querySelector("[data-sync-secondaries]"),form=document.getElementById("recordForm"),table=document.querySelector("table"),filter=document.querySelector("[data-record-filter]");
                    const filterValue=kind === "clients" ? "192.168.1.80" : "quick";
                    filter.value=filterValue;filter.dispatchEvent(new Event("input"));
                    const target=document.querySelector('[data-target="2"]');target.checked=false;target.dispatchEvent(new Event("change"));
                    assert(syncButton.disabled,"Resource sync accepted no selected secondaries");
                    target.checked=true;target.dispatchEvent(new Event("change"));
                    assert(!syncButton.disabled,"Resource sync ignored selected secondary");
                    window.confirm=()=>false;syncButton.click();
                    await wait(()=>!syncButton.disabled && document.querySelector("[data-record-sync-status]").textContent===t("syncCancelled"));
                    let secondary=recordItems(kind,await api(serverById("2"),RECORD_PATHS[kind]));
                    assert(!secondary.some(item=>recordKey(kind,item)===recordKey(kind,source)),"Cancelling resource sync mutated the secondary");
                    const originalPreview=window.nativePreviewSync;let previewArgs,confirmation;
                    window.nativePreviewSync=(...args)=>{previewArgs=args;return originalPreview(...args)};
                    window.confirm=message=>{confirmation=message;return true};
                    syncButton.click();await wait(()=>!syncButton.disabled && document.querySelector("[data-record-sync-status]").textContent.startsWith("Applied:"));
                    window.nativePreviewSync=originalPreview;
                    assert(JSON.stringify(previewArgs)==='[["2"],["'+kind+'"],false]',"Resource sync scope included another resource, main target, or mirror deletions");
                    assert(confirmation.includes("One") && confirmation.includes("Two") && confirmation.includes(t(kind)),"Resource sync confirmation omits direction or resource");
                    secondary=recordItems(kind,await api(serverById("2"),RECORD_PATHS[kind]));
                    const copied=secondary.find(item=>recordKey(kind,item)===recordKey(kind,source));
                    assert(copied && (kind === "hosts" ? copied.ip===source.ip : copied.comment===source.comment && copied.groups[0]===3),"Resource sync did not copy main values and group assignments: "+kind);
                    assert(secondary.some(item=>recordKey(kind,item)===recordKey(kind,extra)),"Resource sync deleted a secondary-only entry");
                    if(kind === "clients")assert(secondary.find(item=>item.client==="192.168.1.50").comment==="Desktop","Resource sync did not update a changed client");
                    assert(document.getElementById("recordForm")===form && document.querySelector("table")===table && filter.value===filterValue,"Resource sync replaced the page or reset the filter");
                    const copiedRow=[...document.querySelectorAll("tbody tr[data-key]")].find(row=>row.dataset.key===recordKey(kind,source));
                    assert(copiedRow && !copiedRow.querySelector('[data-record-server="1"]').textContent.includes(t("missing")),"Resource sync left stale main-server values in the table: "+kind);
                    if(kind === "domains")assert(document.querySelector("[data-record-sync-errors]").textContent.includes("simulated partial failure"),"Resource sync concealed a partial failure");
                    if(kind === "hosts")assert(secondary.find(item=>item.name==="nas.home").ip==="192.168.1.3","Resource sync did not replace changed DNS values");
                }
                await switchTab("gravity");
                document.getElementById("gravBtn1").click();
                await wait(()=>document.getElementById("gravLog1").textContent.includes("Gravity complete ✓"));
                await wait(()=>!gravityState.get("1").running);
                const originalRequest=window.nativeRequest,gravityCalls=[];
                let releaseGravity;
                const gravityGate=new Promise(resolve=>releaseGravity=resolve);
                window.nativeRequest=async(...args)=>{if(args[1].startsWith("/action/gravity")){gravityCalls.push(args[0]);await gravityGate;}return originalRequest(...args)};
                document.getElementById("gravBtn1").click();
                document.querySelector("[data-gravity-all]").click();
                assert(gravityCalls.join(",")==="1,2" && document.querySelector("[data-gravity-all]").disabled,"All-server Gravity missed a server or restarted a running update");
                await switchTab("dashboard");releaseGravity();
                await wait(()=>!gravityState.get("1").running && !gravityState.get("2").running);
                await switchTab("gravity");
                assert(["1","2"].every(id=>document.getElementById("gravLog"+id).textContent.includes("Gravity complete ✓")),"All-server Gravity lost per-host logs after switching views");
                window.nativeRequest=async(...args)=>{if(args[1].startsWith("/action/gravity")){gravityCalls.push(args[0]);if(args[0]==="1")return {status:500};}return originalRequest(...args)};
                state.servers[1].connected=false;await state.page.refresh();
                document.querySelector("[data-gravity-all]").click();
                await wait(()=>!gravityState.get("1").running);
                assert(gravityCalls.join(",")==="1,2,1" && document.getElementById("gravLog1").textContent.includes("500"),"All-server Gravity ran a logged-out host or concealed an error");
                state.servers[1].connected=true;await state.page.refresh();
                document.querySelector("[data-gravity-all]").click();
                await wait(()=>!gravityState.get("1").running && !gravityState.get("2").running);
                assert(gravityCalls.join(",")==="1,2,1,1,2" && document.getElementById("gravLog2").textContent.includes("Gravity complete ✓"),"A failed Gravity update prevented another host from completing");
                window.nativeRequest=originalRequest;
                await switchTab("settings");
                document.querySelector('[data-server="1"] [data-name]').value="Updated desktop name";
                document.getElementById("desktopSettings").requestSubmit();
                await wait(()=>desktopConfig.servers[0].label==="Updated desktop name");
                assert(document.querySelectorAll("[data-settings-logout]").length===2,"Settings must contain logout controls");
                document.querySelector('[data-settings-logout="1"]').click();
                await wait(()=>!state.servers[0].connected);
                assert(!document.querySelector('[data-settings-logout="1"]') && document.querySelector('[data-loginform="1"]'),"Settings logout did not update locally");
                const savedLogin=document.querySelector('[data-loginform="1"] input');savedLogin.value="saved-one";
                state.page.drawHealth();assert(document.querySelector('[data-loginform="1"] input')===savedLogin && savedLogin.value==="saved-one","Health refresh reset a typed password");
                document.querySelector('[data-loginform="1"]').requestSubmit();
                await wait(()=>state.servers[0].connected && document.querySelector('[data-settings-logout="1"]'));
                for(let n=2;n<10;n++)document.getElementById("desktopAdd").click();
                assert(document.querySelectorAll("#desktopServers [data-server]").length===10 && document.getElementById("desktopAdd").disabled,"Settings must allow ten servers");
                const role=document.querySelector('[data-server="10"] [data-role]');role.value="main";role.dispatchEvent(new Event("change",{bubbles:true}));
                assert(document.querySelector('[data-server="1"] [data-role]').value==="secondary","Choosing a main left two main servers");
                document.getElementById("desktopSettings").requestSubmit();await wait(()=>state.servers.length===10 && state.servers[9].role==="main");
                try {await fetch("/api10/auth");throw new Error("Unconfigured host 10 accepted")}catch(error){assert(error.message.includes("Settings"),"Transport did not route server ID 10")}
                for(const width of [400,650,1000]) {
                    await smokeResize(width,1000);await wait(()=>Math.abs(innerWidth-width)<5);
                    for(const tab of ["settings","dashboard","queries","domains","hosts","clients","lists","gravity","drift"]) {
                        await switchTab(tab);
                        const main=document.getElementById("tabContent"),header=document.querySelector("header");
                        assert(main.scrollWidth<=innerWidth+1,"Page overflow at "+width+"/"+tab+": "+main.scrollWidth);
                        assert(header.scrollWidth<=innerWidth+1,"Header overflow at "+width+"/"+tab);
                    }
                }
                await smokeResize(2400,1000);await wait(()=>innerWidth>=2300);await switchTab("settings");
                document.querySelector('[data-remove="10"]').click();
                assert(document.querySelector('[data-server="1"] [data-role]').value==="main","Removing main did not elect a replacement");
                for(let n=3;n<10;n++)document.querySelector('[data-remove="'+n+'"]').click();
                document.getElementById("desktopSettings").requestSubmit();await wait(()=>state.servers.length===2);
                await switchTab("dashboard");
                assert(!document.querySelector("[data-logout],[data-settings-logout]"),"Logout exists outside Settings");
                await nativePreference("ph_combo_theme","light");
                if (errors.length) throw new Error(errors.join("; "));
                await smokeReport("ok");
            } catch (error) { await smokeReport(String(error)+"\n"+String(error.stack || "")); }
        });`)
	w.SetHtml(desktopHTML(a.config.Get()))
	timer := time.AfterFunc(60*time.Second, func() {
		select {
		case result <- "native window timed out":
		default:
		}
		w.Terminate()
	})
	defer timer.Stop()
	w.Run()
	if message := <-result; message != "ok" {
		t.Fatal(message)
	}
	if a.config.Get().Servers[0].Label != "Updated desktop name" || a.config.Get().Servers[0].Password != "saved-one" || a.config.Get().Preferences["ph_combo_theme"] != "light" {
		t.Fatal("desktop settings were not persisted")
	}
}
