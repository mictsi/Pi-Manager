/* Native desktop transport. All Pi-hole requests pass through Go. */
(() => {
    // The native wrapper rejects with JSON strings; give the dashboard the
    // Error objects its existing error handlers expect.
    for (const name of ["nativeRequest", "nativeLogin", "nativeReadStream", "nativeCancelStream", "nativeSaveServers", "nativeBackupServer", "nativeBackupConfig", "nativeRestoreServer", "nativeRestoreConfig", "nativePreference", "nativeUIError", "nativePreviewSync", "nativeApplySync", "nativeCopyRecord", "nativeMonitor", "nativeTrayStatus", "nativeExitReady", "nativeExitDecision"]) {
        const invoke = window[name];
        window[name] = async (...args) => {
            try { return await invoke(...args); }
            catch (error) { throw error instanceof Error ? error : new Error(String(error)); }
        };
    }
    let pendingPreference = Promise.resolve();
    function storage(initial, persistent) {
        const data = new Map(Object.entries(initial));
        return {
            getItem: key => data.has(key) ? data.get(key) : null,
            setItem(key, value) {
                data.set(key, String(value));
                if (persistent && key !== "ph_combo_cfg_v2") {
                    pendingPreference = pendingPreference
                        .then(() => nativePreference(key, String(value)))
                        .catch(error => window.alert(t("saveError") + ": " + humanError(error)));
                }
            },
            removeItem: key => data.delete(key),
            clear: () => data.clear(),
            replace: values => { data.clear(); for (const [key,value] of Object.entries(values)) data.set(key,String(value)); }
        };
    }
    const prefs = {...desktopConfig.preferences};
    prefs.ph_combo_cfg_v2 = JSON.stringify({servers: desktopConfig.servers});
    Object.defineProperty(window, "localStorage", {value: storage(prefs, true)});
    Object.defineProperty(window, "sessionStorage", {value: storage({}, false)});

    window.restoreAppSettings = async contents => {
        await pendingPreference;
        const result = await nativeRestoreConfig(contents);
        localStorage.replace({...result.config.preferences, ph_combo_cfg_v2:JSON.stringify({servers:result.config.servers})});
        sessionStorage.clear();
        return result;
    };

    window.fetch = async (path, options = {}) => {
        const match = String(path).match(/^\/api(10|[1-9])(\/.*)$/);
        if (!match) throw new Error("Only configured Pi-hole API requests are supported");
        const streaming = match[2].startsWith("/action/gravity");
        const r = await nativeRequest(match[1], match[2], options.method || "GET", options.body || "", streaming);
        let body = r.status === 204 ? null : r.body;
        if (r.stream) {
            body = new ReadableStream({
                async pull(controller) {
                    try {
                        const chunk = await nativeReadStream(r.stream);
                        if (chunk.data) controller.enqueue(Uint8Array.from(atob(chunk.data), c => c.charCodeAt(0)));
                        if (chunk.done) controller.close();
                    } catch (error) {
                        await nativeCancelStream(r.stream);
                        controller.error(error);
                    }
                },
                cancel() { return nativeCancelStream(r.stream); }
            });
        }
        return new Response(body, {status: r.status, headers: {"content-type": r.contentType || "text/plain"}});
    };
    window.addEventListener("error", () => nativeUIError());
    window.addEventListener("unhandledrejection", () => nativeUIError());
    document.addEventListener("click", event => {
        if (event.target.closest("a[href]")) event.preventDefault();
    });
})();
