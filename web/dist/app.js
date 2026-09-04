var Ze=globalThis,We=Ze.ShadowRoot&&(Ze.ShadyCSS===void 0||Ze.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,jt=Symbol(),ir=new WeakMap,$e=class{constructor(e,t,o){if(this._$cssResult$=!0,o!==jt)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(We&&e===void 0){let o=t!==void 0&&t.length===1;o&&(e=ir.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),o&&ir.set(t,e))}return e}toString(){return this.cssText}},ar=r=>new $e(typeof r=="string"?r:r+"",void 0,jt),$=(r,...e)=>{let t=r.length===1?r[0]:e.reduce((o,i,a)=>o+(s=>{if(s._$cssResult$===!0)return s.cssText;if(typeof s=="number")return s;throw Error("Value passed to 'css' function must be a 'css' function result: "+s+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(i)+r[a+1],r[0]);return new $e(t,r,jt)},sr=(r,e)=>{if(We)r.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet);else for(let t of e){let o=document.createElement("style"),i=Ze.litNonce;i!==void 0&&o.setAttribute("nonce",i),o.textContent=t.cssText,r.appendChild(o)}},$t=We?r=>r:r=>r instanceof CSSStyleSheet?(e=>{let t="";for(let o of e.cssRules)t+=o.cssText;return ar(t)})(r):r;var{is:Ii,defineProperty:Bi,getOwnPropertyDescriptor:qi,getOwnPropertyNames:Vi,getOwnPropertySymbols:Zi,getPrototypeOf:Wi}=Object,Fe=globalThis,nr=Fe.trustedTypes,Fi=nr?nr.emptyScript:"",Ki=Fe.reactiveElementPolyfillSupport,xe=(r,e)=>r,xt={toAttribute(r,e){switch(e){case Boolean:r=r?Fi:null;break;case Object:case Array:r=r==null?r:JSON.stringify(r)}return r},fromAttribute(r,e){let t=r;switch(e){case Boolean:t=r!==null;break;case Number:t=r===null?null:Number(r);break;case Object:case Array:try{t=JSON.parse(r)}catch{t=null}}return t}},Ke=(r,e)=>!Ii(r,e),lr={attribute:!0,type:String,converter:xt,reflect:!1,useDefault:!1,hasChanged:Ke};Symbol.metadata??=Symbol("metadata"),Fe.litPropertyMetadata??=new WeakMap;var M=class extends HTMLElement{static addInitializer(e){this._$Ei(),(this.l??=[]).push(e)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(e,t=lr){if(t.state&&(t.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(e)&&((t=Object.create(t)).wrapped=!0),this.elementProperties.set(e,t),!t.noAccessor){let o=Symbol(),i=this.getPropertyDescriptor(e,o,t);i!==void 0&&Bi(this.prototype,e,i)}}static getPropertyDescriptor(e,t,o){let{get:i,set:a}=qi(this.prototype,e)??{get(){return this[t]},set(s){this[t]=s}};return{get:i,set(s){let h=i?.call(this);a?.call(this,s),this.requestUpdate(e,h,o)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)??lr}static _$Ei(){if(this.hasOwnProperty(xe("elementProperties")))return;let e=Wi(this);e.finalize(),e.l!==void 0&&(this.l=[...e.l]),this.elementProperties=new Map(e.elementProperties)}static finalize(){if(this.hasOwnProperty(xe("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(xe("properties"))){let t=this.properties,o=[...Vi(t),...Zi(t)];for(let i of o)this.createProperty(i,t[i])}let e=this[Symbol.metadata];if(e!==null){let t=litPropertyMetadata.get(e);if(t!==void 0)for(let[o,i]of t)this.elementProperties.set(o,i)}this._$Eh=new Map;for(let[t,o]of this.elementProperties){let i=this._$Eu(t,o);i!==void 0&&this._$Eh.set(i,t)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let o=new Set(e.flat(1/0).reverse());for(let i of o)t.unshift($t(i))}else e!==void 0&&t.push($t(e));return t}static _$Eu(e,t){let o=t.attribute;return o===!1?void 0:typeof o=="string"?o:typeof e=="string"?e.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(e=>e(this))}addController(e){(this._$EO??=new Set).add(e),this.renderRoot!==void 0&&this.isConnected&&e.hostConnected?.()}removeController(e){this._$EO?.delete(e)}_$E_(){let e=new Map,t=this.constructor.elementProperties;for(let o of t.keys())this.hasOwnProperty(o)&&(e.set(o,this[o]),delete this[o]);e.size>0&&(this._$Ep=e)}createRenderRoot(){let e=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return sr(e,this.constructor.elementStyles),e}connectedCallback(){this.renderRoot??=this.createRenderRoot(),this.enableUpdating(!0),this._$EO?.forEach(e=>e.hostConnected?.())}enableUpdating(e){}disconnectedCallback(){this._$EO?.forEach(e=>e.hostDisconnected?.())}attributeChangedCallback(e,t,o){this._$AK(e,o)}_$ET(e,t){let o=this.constructor.elementProperties.get(e),i=this.constructor._$Eu(e,o);if(i!==void 0&&o.reflect===!0){let a=(o.converter?.toAttribute!==void 0?o.converter:xt).toAttribute(t,o.type);this._$Em=e,a==null?this.removeAttribute(i):this.setAttribute(i,a),this._$Em=null}}_$AK(e,t){let o=this.constructor,i=o._$Eh.get(e);if(i!==void 0&&this._$Em!==i){let a=o.getPropertyOptions(i),s=typeof a.converter=="function"?{fromAttribute:a.converter}:a.converter?.fromAttribute!==void 0?a.converter:xt;this._$Em=i;let h=s.fromAttribute(t,a.type);this[i]=h??this._$Ej?.get(i)??h,this._$Em=null}}requestUpdate(e,t,o,i=!1,a){if(e!==void 0){let s=this.constructor;if(i===!1&&(a=this[e]),o??=s.getPropertyOptions(e),!((o.hasChanged??Ke)(a,t)||o.useDefault&&o.reflect&&a===this._$Ej?.get(e)&&!this.hasAttribute(s._$Eu(e,o))))return;this.C(e,t,o)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(e,t,{useDefault:o,reflect:i,wrapped:a},s){o&&!(this._$Ej??=new Map).has(e)&&(this._$Ej.set(e,s??t??this[e]),a!==!0||s!==void 0)||(this._$AL.has(e)||(this.hasUpdated||o||(t=void 0),this._$AL.set(e,t)),i===!0&&this._$Em!==e&&(this._$Eq??=new Set).add(e))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??=this.createRenderRoot(),this._$Ep){for(let[i,a]of this._$Ep)this[i]=a;this._$Ep=void 0}let o=this.constructor.elementProperties;if(o.size>0)for(let[i,a]of o){let{wrapped:s}=a,h=this[i];s!==!0||this._$AL.has(i)||h===void 0||this.C(i,void 0,a,h)}}let e=!1,t=this._$AL;try{e=this.shouldUpdate(t),e?(this.willUpdate(t),this._$EO?.forEach(o=>o.hostUpdate?.()),this.update(t)):this._$EM()}catch(o){throw e=!1,this._$EM(),o}e&&this._$AE(t)}willUpdate(e){}_$AE(e){this._$EO?.forEach(t=>t.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(e){return!0}update(e){this._$Eq&&=this._$Eq.forEach(t=>this._$ET(t,this[t])),this._$EM()}updated(e){}firstUpdated(e){}};M.elementStyles=[],M.shadowRootOptions={mode:"open"},M[xe("elementProperties")]=new Map,M[xe("finalized")]=new Map,Ki?.({ReactiveElement:M}),(Fe.reactiveElementVersions??=[]).push("2.1.2");var Ct=globalThis,cr=r=>r,Ye=Ct.trustedTypes,hr=Ye?Ye.createPolicy("lit-html",{createHTML:r=>r}):void 0,mr="$lit$",V=`lit$${Math.random().toFixed(9).slice(2)}$`,gr="?"+V,Yi=`<${gr}>`,X=document,we=()=>X.createComment(""),ke=r=>r===null||typeof r!="object"&&typeof r!="function",zt=Array.isArray,Gi=r=>zt(r)||typeof r?.[Symbol.iterator]=="function",_t=`[ 	
\f\r]`,_e=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,dr=/-->/g,pr=/>/g,G=RegExp(`>|${_t}(?:([^\\s"'>=/]+)(${_t}*=${_t}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),ur=/'/g,br=/"/g,fr=/^(?:script|style|textarea|title)$/i,Tt=r=>(e,...t)=>({_$litType$:r,strings:e,values:t}),n=Tt(1),Da=Tt(2),Ha=Tt(3),D=Symbol.for("lit-noChange"),p=Symbol.for("lit-nothing"),vr=new WeakMap,Q=X.createTreeWalker(X,129);function yr(r,e){if(!zt(r)||!r.hasOwnProperty("raw"))throw Error("invalid template strings array");return hr!==void 0?hr.createHTML(e):e}var Qi=(r,e)=>{let t=r.length-1,o=[],i,a=e===2?"<svg>":e===3?"<math>":"",s=_e;for(let h=0;h<t;h++){let l=r[h],c,b,u=-1,m=0;for(;m<l.length&&(s.lastIndex=m,b=s.exec(l),b!==null);)m=s.lastIndex,s===_e?b[1]==="!--"?s=dr:b[1]!==void 0?s=pr:b[2]!==void 0?(fr.test(b[2])&&(i=RegExp("</"+b[2],"g")),s=G):b[3]!==void 0&&(s=G):s===G?b[0]===">"?(s=i??_e,u=-1):b[1]===void 0?u=-2:(u=s.lastIndex-b[2].length,c=b[1],s=b[3]===void 0?G:b[3]==='"'?br:ur):s===br||s===ur?s=G:s===dr||s===pr?s=_e:(s=G,i=void 0);let f=s===G&&r[h+1].startsWith("/>")?" ":"";a+=s===_e?l+Yi:u>=0?(o.push(c),l.slice(0,u)+mr+l.slice(u)+V+f):l+V+(u===-2?h:f)}return[yr(r,a+(r[t]||"<?>")+(e===2?"</svg>":e===3?"</math>":"")),o]},Ae=class r{constructor({strings:e,_$litType$:t},o){let i;this.parts=[];let a=0,s=0,h=e.length-1,l=this.parts,[c,b]=Qi(e,t);if(this.el=r.createElement(c,o),Q.currentNode=this.el.content,t===2||t===3){let u=this.el.content.firstChild;u.replaceWith(...u.childNodes)}for(;(i=Q.nextNode())!==null&&l.length<h;){if(i.nodeType===1){if(i.hasAttributes())for(let u of i.getAttributeNames())if(u.endsWith(mr)){let m=b[s++],f=i.getAttribute(u).split(V),L=/([.?@])?(.*)/.exec(m);l.push({type:1,index:a,name:L[2],strings:f,ctor:L[1]==="."?kt:L[1]==="?"?At:L[1]==="@"?St:pe}),i.removeAttribute(u)}else u.startsWith(V)&&(l.push({type:6,index:a}),i.removeAttribute(u));if(fr.test(i.tagName)){let u=i.textContent.split(V),m=u.length-1;if(m>0){i.textContent=Ye?Ye.emptyScript:"";for(let f=0;f<m;f++)i.append(u[f],we()),Q.nextNode(),l.push({type:2,index:++a});i.append(u[m],we())}}}else if(i.nodeType===8)if(i.data===gr)l.push({type:2,index:a});else{let u=-1;for(;(u=i.data.indexOf(V,u+1))!==-1;)l.push({type:7,index:a}),u+=V.length-1}a++}}static createElement(e,t){let o=X.createElement("template");return o.innerHTML=e,o}};function de(r,e,t=r,o){if(e===D)return e;let i=o!==void 0?t._$Co?.[o]:t._$Cl,a=ke(e)?void 0:e._$litDirective$;return i?.constructor!==a&&(i?._$AO?.(!1),a===void 0?i=void 0:(i=new a(r),i._$AT(r,t,o)),o!==void 0?(t._$Co??=[])[o]=i:t._$Cl=i),i!==void 0&&(e=de(r,i._$AS(r,e.values),i,o)),e}var wt=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){let{el:{content:t},parts:o}=this._$AD,i=(e?.creationScope??X).importNode(t,!0);Q.currentNode=i;let a=Q.nextNode(),s=0,h=0,l=o[0];for(;l!==void 0;){if(s===l.index){let c;l.type===2?c=new Se(a,a.nextSibling,this,e):l.type===1?c=new l.ctor(a,l.name,l.strings,this,e):l.type===6&&(c=new Et(a,this,e)),this._$AV.push(c),l=o[++h]}s!==l?.index&&(a=Q.nextNode(),s++)}return Q.currentNode=X,i}p(e){let t=0;for(let o of this._$AV)o!==void 0&&(o.strings!==void 0?(o._$AI(e,o,t),t+=o.strings.length-2):o._$AI(e[t])),t++}},Se=class r{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(e,t,o,i){this.type=2,this._$AH=p,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=o,this.options=i,this._$Cv=i?.isConnected??!0}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=de(this,e,t),ke(e)?e===p||e==null||e===""?(this._$AH!==p&&this._$AR(),this._$AH=p):e!==this._$AH&&e!==D&&this._(e):e._$litType$!==void 0?this.$(e):e.nodeType!==void 0?this.T(e):Gi(e)?this.k(e):this._(e)}O(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}T(e){this._$AH!==e&&(this._$AR(),this._$AH=this.O(e))}_(e){this._$AH!==p&&ke(this._$AH)?this._$AA.nextSibling.data=e:this.T(X.createTextNode(e)),this._$AH=e}$(e){let{values:t,_$litType$:o}=e,i=typeof o=="number"?this._$AC(e):(o.el===void 0&&(o.el=Ae.createElement(yr(o.h,o.h[0]),this.options)),o);if(this._$AH?._$AD===i)this._$AH.p(t);else{let a=new wt(i,this),s=a.u(this.options);a.p(t),this.T(s),this._$AH=a}}_$AC(e){let t=vr.get(e.strings);return t===void 0&&vr.set(e.strings,t=new Ae(e)),t}k(e){zt(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,o,i=0;for(let a of e)i===t.length?t.push(o=new r(this.O(we()),this.O(we()),this,this.options)):o=t[i],o._$AI(a),i++;i<t.length&&(this._$AR(o&&o._$AB.nextSibling,i),t.length=i)}_$AR(e=this._$AA.nextSibling,t){for(this._$AP?.(!1,!0,t);e!==this._$AB;){let o=cr(e).nextSibling;cr(e).remove(),e=o}}setConnected(e){this._$AM===void 0&&(this._$Cv=e,this._$AP?.(e))}},pe=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(e,t,o,i,a){this.type=1,this._$AH=p,this._$AN=void 0,this.element=e,this.name=t,this._$AM=i,this.options=a,o.length>2||o[0]!==""||o[1]!==""?(this._$AH=Array(o.length-1).fill(new String),this.strings=o):this._$AH=p}_$AI(e,t=this,o,i){let a=this.strings,s=!1;if(a===void 0)e=de(this,e,t,0),s=!ke(e)||e!==this._$AH&&e!==D,s&&(this._$AH=e);else{let h=e,l,c;for(e=a[0],l=0;l<a.length-1;l++)c=de(this,h[o+l],t,l),c===D&&(c=this._$AH[l]),s||=!ke(c)||c!==this._$AH[l],c===p?e=p:e!==p&&(e+=(c??"")+a[l+1]),this._$AH[l]=c}s&&!i&&this.j(e)}j(e){e===p?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},kt=class extends pe{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===p?void 0:e}},At=class extends pe{constructor(){super(...arguments),this.type=4}j(e){this.element.toggleAttribute(this.name,!!e&&e!==p)}},St=class extends pe{constructor(e,t,o,i,a){super(e,t,o,i,a),this.type=5}_$AI(e,t=this){if((e=de(this,e,t,0)??p)===D)return;let o=this._$AH,i=e===p&&o!==p||e.capture!==o.capture||e.once!==o.once||e.passive!==o.passive,a=e!==p&&(o===p||i);i&&this.element.removeEventListener(this.name,this,o),a&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,e):this._$AH.handleEvent(e)}},Et=class{constructor(e,t,o){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=o}get _$AU(){return this._$AM._$AU}_$AI(e){de(this,e)}};var Xi=Ct.litHtmlPolyfillSupport;Xi?.(Ae,Se),(Ct.litHtmlVersions??=[]).push("3.3.3");var jr=(r,e,t)=>{let o=t?.renderBefore??e,i=o._$litPart$;if(i===void 0){let a=t?.renderBefore??null;o._$litPart$=i=new Se(e.insertBefore(we(),a),a,void 0,t??{})}return i._$AI(r),i};var Lt=globalThis,j=class extends M{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){let e=super.createRenderRoot();return this.renderOptions.renderBefore??=e.firstChild,e}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=jr(t,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return D}};j._$litElement$=!0,j.finalized=!0,Lt.litElementHydrateSupport?.({LitElement:j});var Ji=Lt.litElementPolyfillSupport;Ji?.({LitElement:j});(Lt.litElementVersions??=[]).push("4.2.2");var $r=new WeakMap,xr=r=>{if((t=>t.pattern!==void 0)(r))return r.pattern;let e=$r.get(r);return e===void 0&&$r.set(r,e=new URLPattern({pathname:r.path})),e},Ee=class{constructor(e,t,o){this.routes=[],this.o=[],this.t={},this.i=i=>{if(i.routes===this)return;let a=i.routes;this.o.push(a),a.h=this,i.stopImmediatePropagation(),i.onDisconnect=()=>{this.o?.splice(this.o.indexOf(a)>>>0,1)};let s=_r(this.t);s!==void 0&&a.goto(s)},(this.l=e).addController(this),this.routes=[...t],this.fallback=o?.fallback}link(e){if(e?.startsWith("/"))return e;if(e?.startsWith("."))throw Error("Not implemented");return e??=this.u,(this.h?.link()??"")+e}async goto(e){let t;if(this.routes.length===0&&this.fallback===void 0)t=e,this.u="",this.t={0:t};else{let o=this.p(e);if(o===void 0)throw Error("No route found for "+e);let i=xr(o).exec({pathname:e}),a=i?.pathname.groups??{};if(t=_r(a),typeof o.enter=="function"&&await o.enter(a)===!1)return;this.v=o,this.t=a,this.u=t===void 0?e:e.substring(0,e.length-t.length)}if(t!==void 0)for(let o of this.o)o.goto(t);this.l.requestUpdate()}outlet(){return this.v?.render?.(this.t)}get params(){return this.t}p(e){let t=this.routes.find(o=>xr(o).test({pathname:e}));return t||this.fallback===void 0?t:this.fallback?{...this.fallback,path:"/*"}:void 0}hostConnected(){this.l.addEventListener(ue.eventName,this.i);let e=new ue(this);this.l.dispatchEvent(e),this._=e.onDisconnect}hostDisconnected(){this._?.(),this.h=void 0}},_r=r=>{let e;for(let t of Object.keys(r))/\d+/.test(t)&&(e===void 0||t>e)&&(e=t);return e&&r[e]},ue=class r extends Event{constructor(e){super(r.eventName,{bubbles:!0,composed:!0,cancelable:!1}),this.routes=e}};ue.eventName="lit-routes-connected";var ea=location.origin||location.protocol+"//"+location.host,Ce=class extends Ee{constructor(){super(...arguments),this.m=e=>{let t=e.button!==0||e.metaKey||e.ctrlKey||e.shiftKey;if(e.defaultPrevented||t)return;let o=e.composedPath().find(s=>s.tagName==="A");if(o===void 0||o.target!==""||o.hasAttribute("download")||o.getAttribute("rel")==="external")return;let i=o.href;if(i===""||i.startsWith("mailto:"))return;let a=window.location;o.origin===ea&&(e.preventDefault(),i!==a.href&&(window.history.pushState({},"",i),this.goto(o.pathname)))},this.R=e=>{this.goto(window.location.pathname)}}hostConnected(){super.hostConnected(),window.addEventListener("click",this.m),window.addEventListener("popstate",this.R),this.goto(window.location.pathname)}hostDisconnected(){super.hostDisconnected(),window.removeEventListener("click",this.m),window.removeEventListener("popstate",this.R)}};function wr(){let r=new URL(location.href),e=r.searchParams.get("token");e&&(document.cookie=`box_token=${encodeURIComponent(e)}; path=/; SameSite=Strict`,r.searchParams.delete("token"),history.replaceState({},"",r.toString()))}var Ge=class extends Error{constructor(e,{status:t,code:o,method:i,path:a}={}){super(e),this.status=t,this.code=o??"internal",this.method=i,this.path=a}};async function be(r,e,t){let o=await fetch(e,{method:r,headers:t?{"Content-Type":"application/json"}:void 0,body:t?JSON.stringify(t):void 0,credentials:"same-origin"});if(!o.ok){let i=`${r} ${e}: ${o.status}`,a;try{let s=await o.json();i=s.error??i,a=s.code}catch{}throw new Ge(i,{status:o.status,code:a,method:r,path:e})}return o.json()}async function H(r,e){let t=await fetch(r,{signal:e,credentials:"same-origin"});if(!t.ok){let o=`${r}: ${t.status}`,i;try{let a=await t.json();o=a.error??o,i=a.code}catch{}throw new Ge(o,{status:t.status,code:i,method:"GET",path:r})}return t.json()}var ze=r=>H("/v1/status",r),Rt=(r,e)=>H("/v1/items"+(r?.length?`?state=${encodeURIComponent(r.join(","))}`:""),e),kr=r=>H("/v1/exceptions",r),Ar=(r,e)=>H(`/v1/items/${encodeURIComponent(r)}`,e),Sr=(r,e)=>H(`/v1/items/${encodeURIComponent(r)}/evidence`,e),Er=(r,e)=>H(`/v1/provenance/${encodeURIComponent(r)}/chain`,e),Cr=r=>`/v1/provenance/${encodeURIComponent(r)}/export`,zr=r=>H("/v1/activity",r),Tr=r=>H("/v1/sessions",r),Lr=(r,e)=>H(`/v1/items/${encodeURIComponent(r)}/session`,e),Rr=(r,e,t)=>be("POST","/v1/ingress",{route:r,key:e,payload:t}),Pr=r=>be("POST","/v1/stop",{reason:r}),Or=r=>be("POST","/v1/resume",{reason:r}),Pt=(r,e)=>be("POST",`/v1/items/${encodeURIComponent(r)}/resolve`,{reason:e}),Ot=(r,e)=>be("POST",`/v1/items/${encodeURIComponent(r)}/retry`,{reason:e}),Nr=()=>be("POST","/v1/reload",{});var _={es:null,subs:new Set,connected:!1};function ta(){_.es=new EventSource("/v1/events"),_.es.addEventListener("hello",()=>{_.connected=!0;for(let r of _.subs)r.onState?.(!0)}),_.es.addEventListener("session",r=>{let e=JSON.parse(r.data);for(let t of _.subs)t.onSession?.(e)}),_.es.addEventListener("activity",r=>{let e=JSON.parse(r.data);for(let t of _.subs)t.onEvent?.(e)}),_.es.onerror=()=>{_.connected=!1;for(let r of _.subs)r.onState?.(!1)}}function oa(r){return _.subs.add(r),_.es?r.onState?.(_.connected):ta(),()=>{_.subs.delete(r),_.subs.size===0&&(_.es?.close(),_.es=null,_.connected=!1)}}var N=class{events=[];connected=!1;constructor(e,{limit:t=100,onEvent:o,onSession:i,item:a}={}){this._host=e,this._limit=t,this._onEvent=o,this._onSession=i,this._item=a,e.addController(this)}hostConnected(){let e={onState:t=>{this.connected=t,this._host.requestUpdate()},onSession:t=>this._onSession?.(t),onEvent:t=>{this.events=[...this.events,t].slice(-this._limit),this._host.requestUpdate(),this._onEvent?.(t)}};if(this._item){this._es=new EventSource(`/v1/events?item=${encodeURIComponent(this._item)}`),this._es.addEventListener("hello",()=>e.onState(!0)),this._es.addEventListener("session",t=>e.onSession(JSON.parse(t.data))),this._es.addEventListener("activity",t=>e.onEvent(JSON.parse(t.data))),this._es.onerror=()=>e.onState(!1);return}this._limit>1&&zr().then(t=>{this.events=(t??[]).slice(-this._limit),this._host.requestUpdate()}).catch(()=>{}),this._unsub=oa(e)}hostDisconnected(){this._es?.close(),this._es=void 0,this._unsub?.(),this._unsub=void 0}};var Qe="box-theme",Nt=[{id:"system",label:"System",icon:"desktop"},{id:"light",label:"Light",icon:"lightbulb"},{id:"dark",label:"Dark",icon:"contrast"}],Ur=matchMedia("(prefers-color-scheme: dark)");function Te(){try{let r=localStorage.getItem(Qe);if(Nt.some(e=>e.id===r))return r}catch{}return"system"}function ra(r=Te()){return r==="dark"||r==="system"&&Ur.matches}function Ut(r=Te()){let e=ra(r);document.documentElement.classList.toggle("jh-theme-dark",e),document.documentElement.style.colorScheme=e?"dark":"light",window.dispatchEvent(new CustomEvent("box-theme",{detail:{theme:r,dark:e}}))}function Mr(r){try{r==="system"?localStorage.removeItem(Qe):localStorage.setItem(Qe,r)}catch{}Ut(r)}Ur.addEventListener("change",()=>{Te()==="system"&&Ut("system")});window.addEventListener("storage",r=>{r.key===Qe&&Ut()});var ia=r=>n`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path fill-rule="evenodd" clip-rule="evenodd" d=${r}></path></svg>`,aa={desktop:"M3.25 4.5A.75.75 0 0 1 4 3.75h16a.75.75 0 0 1 .75.75v12a.75.75 0 0 1-.75.75h-4.96l.671 2.014a.75.75 0 0 1-.711.987H9a.75.75 0 0 1-.712-.987l.671-2.013H4a.75.75 0 0 1-.75-.75v-12Zm1.5 9.75v1.5h14.5v-1.5H4.75Zm14.5-1.5H4.75v-7.5h14.5v7.5Zm-8.71 4.5h2.92l.5 1.5h-3.92l.5-1.5Z",lightbulb:"M12 3.75A5.25 5.25 0 0 0 6.75 9c0 2.333 1.504 4.588 3.5 5.293.3.106.5.39.5.707v1.24h2.5V15a.75.75 0 0 1 .5-.707c1.996-.706 3.5-2.96 3.5-5.293 0-2.9-2.35-5.25-5.25-5.25ZM5.25 9a6.75 6.75 0 0 1 13.5 0c0 2.707-1.608 5.398-4 6.504v1.486a.75.75 0 0 1-.75.75h-4a.75.75 0 0 1-.75-.75v-1.486c-2.391-1.106-4-3.797-4-6.504Zm4 10a.75.75 0 0 1 .75-.75h4a.75.75 0 1 1 0 1.5h-4a.75.75 0 0 1-.75-.75ZM11 20.25a.75.75 0 1 0 0 1.5h2a.75.75 0 0 0 0-1.5h-2Z",contrast:"M12 3.75v16.5a8.25 8.25 0 0 0 0-16.5Zm0-1.5c-5.385 0-9.75 4.366-9.75 9.75 0 5.385 4.365 9.75 9.75 9.75s9.75-4.365 9.75-9.75c0-5.384-4.365-9.75-9.75-9.75Z",stop:"M3.75 12a8.25 8.25 0 1 1 16.5 0 8.25 8.25 0 0 1-16.5 0ZM12 2.25c-5.385 0-9.75 4.366-9.75 9.75 0 5.385 4.365 9.75 9.75 9.75s9.75-4.365 9.75-9.75c0-5.384-4.365-9.75-9.75-9.75Zm-3.5 5.5a.75.75 0 0 0-.75.75v7c0 .415.336.75.75.75h7a.75.75 0 0 0 .75-.75v-7a.75.75 0 0 0-.75-.75h-7Zm.75 7v-5.5h5.5v5.5h-5.5Z",play:"M3.75 12a8.25 8.25 0 1 1 16.5 0 8.25 8.25 0 0 1-16.5 0ZM12 2.25c-5.385 0-9.75 4.366-9.75 9.75 0 5.385 4.365 9.75 9.75 9.75s9.75-4.365 9.75-9.75c0-5.384-4.365-9.75-9.75-9.75Zm4.759 10.46a.75.75 0 0 0 0-1.207l-6.515-4.8a.75.75 0 0 0-1.195.605v9.598a.75.75 0 0 0 1.195.604l6.515-4.8Zm-6.21 2.712v-6.63l4.5 3.315-4.5 3.315Z",rotate:"M12.024 3.25C7.18 3.25 3.25 7.166 3.25 12A8.741 8.741 0 0 0 12 20.75c4.37 0 7.634-3.28 8.711-6.513a.75.75 0 0 0-1.423-.474C18.366 16.53 15.582 19.25 12 19.25A7.241 7.241 0 0 1 4.75 12c0-4.002 3.254-7.25 7.274-7.25a7.275 7.275 0 0 1 6.228 3.5H16a.75.75 0 0 0 0 1.5h4.75V5a.75.75 0 0 0-1.5 0v2.035a8.772 8.772 0 0 0-7.226-3.785Z",check:"M12 3.75a8.25 8.25 0 1 0 0 16.5 8.25 8.25 0 0 0 0-16.5ZM2.25 12c0-5.384 4.365-9.75 9.75-9.75s9.75 4.366 9.75 9.75c0 5.385-4.365 9.75-9.75 9.75S2.25 17.386 2.25 12Zm13.755-3.005a.75.75 0 0 1 0 1.06l-4.95 4.95-.015.016-.01.01a.75.75 0 0 1-1.06 0l-2.5-2.5a.75.75 0 0 1 1.06-1.06l1.97 1.969 4.444-4.445a.75.75 0 0 1 1.061 0Z",external:"M13.658 3.921a.75.75 0 0 0 0 1.5h3.846l-6.548 6.549a.75.75 0 1 0 1.06 1.06l6.548-6.547v3.845a.75.75 0 0 0 1.5 0V3.921h-6.406ZM3.236 7a.75.75 0 0 1 .75-.75h6a.75.75 0 0 1 0 1.5h-5.25v11.5h11.5V15a.75.75 0 0 1 1.5 0v5a.75.75 0 0 1-.75.75h-13a.75.75 0 0 1-.75-.75V7Z",warning:"M12.598 5.066c-.26-.422-.937-.422-1.197 0L7.61 11.225l-3.773 6.127c-.211.343.006.898.599.898h15.13c.593 0 .81-.555.6-.898l-3.774-6.127L12.6 5.066Zm1.278-.786c-.845-1.374-2.906-1.374-3.752 0l-3.792 6.158-3.773 6.128c-.903 1.466.257 3.184 1.876 3.184h15.13c1.618 0 2.78-1.718 1.876-3.184l-3.773-6.128-3.792-6.158ZM12 7.25a.75.75 0 0 1 .75.75v4a.75.75 0 1 1-1.5 0V8a.75.75 0 0 1 .75-.75Zm0 7.25a1 1 0 1 0 0 2 1 1 0 0 0 0-2Z",info:"M12 3.75a8.25 8.25 0 1 0 0 16.5 8.25 8.25 0 0 0 0-16.5ZM2.25 12c0-5.384 4.365-9.75 9.75-9.75s9.75 4.366 9.75 9.75c0 5.385-4.365 9.75-9.75 9.75S2.25 17.386 2.25 12ZM12 10.25a.75.75 0 0 1 .75.75v5a.75.75 0 0 1-1.5 0v-5a.75.75 0 0 1 .75-.75Zm0-1.75a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z",dot:"M3.75 12a8.25 8.25 0 1 1 16.5 0 8.25 8.25 0 0 1-16.5 0ZM12 2.25c-5.385 0-9.75 4.366-9.75 9.75 0 5.385 4.365 9.75 9.75 9.75s9.75-4.365 9.75-9.75c0-5.384-4.365-9.75-9.75-9.75ZM12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10Z",checkmark:"M19.96 7.53a.75.75 0 0 0-1.062-1.06l-9.47 9.47-3.469-3.47a.75.75 0 1 0-1.06 1.06l4 4a.75.75 0 0 0 1.06 0",xmark:"M6.53 5.47a.75.75 0 0 0-1.06 1.06L10.94 12l-5.47 5.47a.75.75 0 0 0 1.06 1.06L12 13.06l5.47 5.47a.75.75 0 0 0 1.06-1.06L13.06 12l5.47-5.47a.75.75 0 1 0-1.06-1.06L12 10.94 6.53 5.47Z",pause:"M12 3.75a8.25 8.25 0 1 0 0 16.5 8.25 8.25 0 0 0 0-16.5ZM2.25 12c0-5.384 4.365-9.75 9.75-9.75s9.75 4.366 9.75 9.75c0 5.385-4.365 9.75-9.75 9.75S2.25 17.386 2.25 12ZM10 7.5a.75.75 0 0 1 .75.75v7a.75.75 0 1 1-1.5 0v-7A.75.75 0 0 1 10 7.5Zm4.75.75a.75.75 0 0 0-1.5 0v7a.75.75 0 1 0 1.5 0v-7Z"},Z=(r,e="small")=>n`<jh-icon size=${e}>${ia(aa[r])}</jh-icon>`;function k(r){return r==null?"":(r=Math.max(0,Math.round(r)),r<90?`${r}s`:r<5400?`${Math.floor(r/60)}m`:r<172800?`${Math.floor(r/3600)}h`:`${Math.floor(r/86400)}d`)}function w(r){r=String(r??"");let e=r.indexOf(`
`);return(e<0?r:r.slice(0,e)).trim()}function Mt(r,e=200){return r=String(r??""),r.length>e?r.slice(0,e-1)+"\u2026":r}function E(r){return w(r)}function Hr(r){let e=String(r??"").match(/https?:\/\/[^\s)>\]]+/);return e?e[0]:""}function I(r,e=10){let t=String(r??"");return/^[0-9a-f]{16,}$/i.test(t)?t.slice(0,e):t}var sa=new Intl.DateTimeFormat(void 0,{dateStyle:"medium",timeStyle:"medium"});function Ir(r){if(!r)return{rel:"",abs:"",iso:""};let e=new Date(r);if(Number.isNaN(e.getTime()))return{rel:String(r),abs:"",iso:""};let t=Math.round((Date.now()-e.getTime())/1e3);return{rel:t<-60?`in ${k(-t)}`:t<5?"just now":`${k(t)} ago`,abs:sa.format(e),iso:e.toISOString()}}function W(r){return r==null?"0":r<1e3?String(r):r<1e6?`${Math.round(r/100)/10}k`:`${Math.round(r/1e5)/10}M`}function Je(r){let e=(r??0)/1e6;return e<1?`$${e.toFixed(4)}`:`$${e.toFixed(2)}`}function et(r){return[["in",r?.input],["out",r?.output],["cache read",r?.cache_read],["cache write",r?.cache_write]].filter(([,t])=>t>0).map(([t,o])=>`${t} ${W(o)}`).join(" \xB7 ")}function Dr(r){return Object.entries(r??{}).sort((e,t)=>t[1]-e[1]).map(([e,t])=>t>1?`${e}\xD7${t}`:e).join(" ")}function ee(r){return r?.route?r.key?`${r.route} / ${r.key}`:r.route:""}function te(r){return[r?.box,r?.repo].filter(Boolean).join(" / ")}function tt(r){if(!r?.runner)return"";let e=r.lease_expires_sec??0;return r.lease_lapsed?`${r.runner} \u2014 lease lapsed ${k(-e)} ago`:`${r.runner} \u2014 ${k(e)} left`}function Dt(r){return r?.blocked_by?.length?r.blocked_by.join(", "):r?.root?`cascaded from ${r.root}`:""}function Br(r){return Object.keys(r??{}).sort().map(e=>`${e} ${r[e]}`).join(" \xB7 ")}var qr={pending:"neutral",claimed:"brand",waiting:"neutral",handedoff:"neutral",parked:"negative",failed:"negative",done:"positive",resolved:"positive"},na={done:"positive",land:"positive",check:"positive",resume:"positive","config-reloaded":"positive",recovered:"positive",resolved:"positive",started:"positive",promoted:"positive","build-resumed":"positive",parked:"negative",failed:"negative","budget-refused":"negative",stop:"negative",denied:"negative",escalated:"negative","land-refused":"negative","land-conflict":"negative","rebase-refused":"negative","handoff-failed":"negative","break-glass":"negative",preempted:"negative",claimed:"brand","agent-run":"brand",retried:"brand"},Ht={low:"neutral",medium:"neutral",high:"negative",critical:"negative"},Vr=r=>na[r]??"neutral",la=new TextDecoder;function ca(r){if(r.body!==void 0&&r.body!==null)return r.body;if(r.Body==null||r.Body==="")return"";let e;try{e=la.decode(Uint8Array.from(atob(r.Body),t=>t.charCodeAt(0)))}catch{return""}try{return JSON.parse(e)}catch{return e}}function Zr(r){return{kind:r.kind??r.Kind??"",subject:r.subject??r.Subject??"",scope:r.scope??r.Scope??"",actor:r.actor??r.Actor??"",at:r.at??r.At??"",seq:r.seq??r.Seq??0,body:ca(r)}}var ha=new Set(["id","tenant","box","repo","ingress","requester","task","state","opened","changed","depth"]);function Xe(r,e=4){return Object.entries(r??{}).filter(([t,o])=>!ha.has(t)&&o!==""&&o!=null&&typeof o!="object").slice(0,e).map(([t,o])=>`${t}: ${I(String(o))}`).join(" \xB7 ")}function J(r){return r?.note?` \u2014 ${w(r.note)}`:""}function z(r,e){let t=r?.reason??r?.Reason??e;return t?w(t):""}function ot(r,e={}){let t=Zr(r),o=t.body&&typeof t.body=="object"?t.body:null,i=typeof t.body=="string"?t.body:"",a=s=>e[s]?E(e[s]):s;switch(t.kind){case"queued":return"Queued"+(o?.depends_on?.length?`, after ${o.depends_on.join(", ")}`:"")+J(o);case"claimed":return`Claimed${o?.seat?` for seat ${o.seat}`:""}${J(o)}`;case"waiting":return`Waiting${J(o)}`;case"handedoff":return`Handed off to a fresh session${J(o)}`;case"parked":return`Parked${J(o)}`;case"failed":return`Failed${J(o)}`;case"recovered":return`Recovered from a lapsed claim${J(o)}`;case"done":return`Done \u2014 landed as ${I(t.subject)}`;case"built":return`Built ${I(o?.built||t.subject)}`;case"land":return o?.branch?`Landed on ${[o.repo,o.branch].filter(Boolean).join("/")}: ${I(o.was)} \u2192 ${I(o.now??t.subject)}`:`Landed ${I(t.subject)}`;case"gate":return`${o?.Gate??o?.gate??"gate"}: ${o?.Decision??o?.decision??""}${z(o)?" \u2014 "+z(o):""}`;case"check":return o&&"green"in o?`Check ${o.green?"green":"RED"}${o.lane?` on the ${o.lane} lane`:""}`:o?`Check ${o.Decision??o.decision??""} ${z(o)}`.trim():`Check ${w(i)}`.trim();case"agent-run":return`${o?.seat??"agent"} started as ${o?.role??"builder"} (mode ${o?.mode??"?"})`;case"agent-prompt":return"Prompt materialized for the agent";case"agent-result":{if(!o)return w(i);let s=[`${o.stop}`,`${Math.round((o.wall_ms??0)/100)/10}s`];return s.push(o.metered?`${W(o.tokens)} tokens`:"cost NOT reported"),o.cost_usd&&s.push(`$${o.cost_usd.toFixed(4)}`),s.push(o.tools?Dr(o.tools):`${o.tool_calls??0} tools`),o.denied_tools&&s.push(`DENIED ${Dr(o.denied_tools)}`),o.unknown_updates?.length&&s.push(`dialect drift: ${o.unknown_updates.join(", ")}`),s.join(" \xB7 ")}case"charge":{if(!o)return w(i);if(!o.metered)return"Cost NOT reported \u2014 claims stopped";let s=[`${W(o.tokens??0)} tokens`],h=et(o);return h&&s.push(h),o.cost_micros&&s.push(Je(o.cost_micros)),s.join(" \xB7 ")}case"spend":return o?.tokens!=null?`Spent ${W(o.tokens)} tokens`:o?.running!=null?`Run started \u2014 ${o.running} running, ${o.started_this_window??"?"} started this window`:`Spend recorded${i?" \u2014 "+w(i):""}`;case"budget-refused":return o?.max_per_window!=null?`Refused by the budget \u2014 ${o.max_per_window} starts per ${o.window??"window"} already used`:o?.max_tokens!=null?`Refused by the budget \u2014 ${W(o.max_tokens)} tokens per window already spent`:`Refused by the budget${z(o,i)?" \u2014 "+z(o,i):Xe(o)?" \u2014 "+Xe(o):""}`;case"authz":return z(o,i)||"Authorization checked";case"escalated":return`Escalated: ${z(o,i)}`;case"handoff-requested":return`Handoff requested: ${z(o,i)}`;case"handoff-failed":return`Handoff failed: ${z(o,i)}`;case"denied":return`Tool refused by policy: ${o?.tool??z(o,i)}`;case"retried":return`Retried by an operator${i?" \u2014 "+w(i):""}`;case"resolved":return`Resolved by an operator${i?" \u2014 "+w(i):""}`;case"stop":return`Claims stopped \u2014 ${w(i)}`;case"resume":return`Claims resumed \u2014 ${w(i)}`;case"config-reloaded":return`Configuration reloaded${i?": "+w(i):o?": "+Xe(o):""}`;case"ingress-open":return`Ingress opened${o?.route?` on ${o.route}`:""}${o?.key?` for ${o.key}`:""}`;case"indexed":return da(t,i,a);case"compacted":return"Chain compacted behind a verifiable seal";case"review-sample":return"Sampled for human review";case"review-signoff":return`Signed off${i?" \u2014 "+w(i):""}`;case"break-glass":return`Break-glass: ${z(o,i)}`;case"preempted":return`Preempted by an incident${z(o,i)?" \u2014 "+z(o,i):""}`;default:return i?Mt(w(i)):o?Mt(Xe(o))||t.kind:t.subject}}function da(r,e,t){let o=r.scope.replace(/^index\//,""),i=e?` \u2014 ${w(e)}`:"";switch(o){case"pending":return`Queued: ${t(r.subject)}${i}`;case"exceptions":return`Parked: ${t(r.subject)}${i}`;case"landings":return`Landed: ${t(r.subject)}${i}`;case"ingress":return`Ingress opened: ${e?w(e):r.subject}`;default:return`Filed under ${o}: ${t(r.subject)}${i}`}}function It(r){let e=Zr(r);if(/^wi-/.test(e.subject))return e.subject;let t=e.scope.match(/^workitem\/(wi-[^/]+)$/);return t?t[1]:""}var C=$`
  a { color: var(--jh-color-content-brand-enabled); text-decoration: underline; text-underline-offset: 2px; }
  a:hover { color: var(--jh-color-content-brand-hover); }
  a:focus-visible, button:focus-visible, [tabindex]:focus-visible {
    outline: none; box-shadow: var(--jh-shadow-focus); border-radius: var(--jh-border-radius-100);
  }
  .vh { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
  .muted { color: var(--jh-color-content-secondary-enabled); }
  .helper { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
  .micro { font: var(--jh-font-micro-medium); color: var(--jh-color-content-secondary-enabled); text-transform: uppercase; letter-spacing: .04em; }
  code, .mono { font: var(--jh-font-code-regular-2); font-size: .85em; }
  .empty { color: var(--jh-color-content-secondary-enabled); margin: 0; }
  /* A tinted container pairs with its own content colour: red-100 under
     red-600 in the light theme, red-800 under red-300 in the dark. The
     "on-" colours are for solid surfaces and vanish on a tint. */
  jh-tag { --jh-tag-color-border-enabled: transparent; }
  jh-tag.positive {
    --jh-tag-color-background-enabled: var(--jh-color-container-positive-enabled);
    --jh-tag-color-text-enabled: var(--jh-color-content-positive-enabled);
  }
  jh-tag.negative {
    --jh-tag-color-background-enabled: var(--jh-color-container-negative-enabled);
    --jh-tag-color-text-enabled: var(--jh-color-content-negative-enabled);
  }
  jh-tag.brand {
    --jh-tag-color-background-enabled: var(--jh-color-container-brand-enabled);
    --jh-tag-color-text-enabled: var(--jh-color-content-brand-enabled);
  }
  jh-notification a { color: inherit; text-decoration: underline; }
  jh-notification { display: block; margin-top: var(--jh-space-300); }
  .actions { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; }
`,pa=r=>String(r??"").replace(/[&<>"']/g,e=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"})[e]);function R(r,e="neutral",t=6e3){window.dispatchEvent(new CustomEvent("jh-create-toast",{detail:{text:pa(r),appearance:e,timeout:t,dismissButtonAccessibleLabel:"Dismiss"}}))}function Wr(r){(r?.shadowRoot?.querySelector("input, textarea")??r)?.focus?.()}var Bt=class extends j{static properties={value:{},label:{},href:{},chars:{type:Number}};static styles=[C,$`
    :host { display: inline-flex; align-items: center; gap: var(--jh-space-100); vertical-align: middle; }
    code { padding: 1px var(--jh-space-100); border-radius: var(--jh-border-radius-100);
           background: var(--jh-color-container-secondary-enabled); }
    a code { color: inherit; }
    button { font: var(--jh-font-micro-medium); cursor: pointer; padding: 2px var(--jh-space-200);
             border: 1px solid var(--jh-color-control-enabled); border-radius: var(--jh-border-radius-pill);
             background: transparent; color: var(--jh-color-content-secondary-enabled); }
    button:hover { color: var(--jh-color-content-primary-enabled); border-color: var(--jh-color-control-hover); }
  `];constructor(){super(),this.chars=10,this.label="id"}async _copy(){try{await navigator.clipboard.writeText(this.value),R(`Copied ${this.label} ${this.value.slice(0,this.chars)}\u2026`,"positive",3e3)}catch{R(`Could not copy \u2014 select the ${this.label} and copy it by hand`,"negative")}}render(){let e=String(this.value??"");if(!e)return p;let t=e.length>this.chars+2?e.slice(0,this.chars):e,o=n`<code title=${e}>${t}</code>`;return n`
      ${this.href?n`<a href=${this.href} aria-label=${`${this.label} ${t}`}>${o}</a>`:o}
      ${e.length>t.length?n`<button type="button" @click=${this._copy} aria-label=${`Copy the full ${this.label}`}>Copy</button>`:p}`}};customElements.define("box-hash",Bt);var qt=class extends j{static properties={at:{},abs:{type:Boolean}};static styles=$`:host { display: inline; } time { font-variant-numeric: tabular-nums; }`;render(){let e=Ir(this.at);return e.iso?n`<time datetime=${e.iso} title=${e.abs}>${this.abs?e.abs:e.rel}</time>`:n`${e.rel}`}};customElements.define("box-when",qt);var Vt=class extends j{static properties={label:{},confirm:{},appearance:{},size:{},prompt:{},glyph:{},floating:{type:Boolean},action:{attribute:!1},_open:{state:!0},_reason:{state:!0},_pending:{state:!0},_error:{state:!0}};static styles=[C,$`
    :host { display: inline-block; position: relative; }
    form { display: grid; gap: var(--jh-space-200); margin-top: var(--jh-space-200);
           padding: var(--jh-space-300); max-width: 420px;
           background: var(--jh-color-container-primary-enabled);
           border: 1px solid var(--jh-color-divider-primary);
           border-radius: var(--jh-border-radius-200); box-shadow: var(--jh-shadow-200); }
    :host([floating]) form { position: absolute; top: calc(100% + var(--jh-space-100)); left: 0; z-index: var(--jh-z-index-positive-300);
                            width: max(320px, 100%); margin-top: 0; }
    .row { display: flex; gap: var(--jh-space-200); }
    jh-notification { margin: 0; }
  `];constructor(){super(),this.appearance="secondary",this.size="small",this._open=!1,this._reason="",this._pending=!1,this._error=""}async _toggle(){this._open=!this._open,this._error="",this._open&&(await this.updateComplete,Wr(this.renderRoot.querySelector("jh-input")))}_cancel(){this._open=!1,this._reason="",this._error="",this.renderRoot.querySelector("jh-button")?.focus()}async _submit(e){e.preventDefault();let t=this._reason.trim();if(!(!t||this._pending)){this._pending=!0,this._error="";try{let o=await this.action(t);this._open=!1,this._reason="",this.dispatchEvent(new CustomEvent("box-done",{detail:{reason:t,result:o},bubbles:!0,composed:!0}))}catch(o){this._error=o.message}finally{this._pending=!1}}}render(){return n`
      <jh-button appearance=${this.appearance} size=${this.size} label=${this.label}
        aria-expanded=${this._open?"true":"false"} @click=${this._toggle}>
        ${this.glyph?n`<span slot="jh-button-icon-left">${Z(this.glyph,"x-small")}</span>`:p}
      </jh-button>
      ${this._open?n`
        <form role="group" aria-label=${this.prompt??this.label} @submit=${this._submit}
          @keydown=${e=>{e.key==="Escape"&&this._cancel()}}>
          <jh-input size="small" label="Reason" required
            helper-text="Becomes the record: who did this, when, and why"
            .value=${this._reason}
            @jh-change=${e=>{this._reason=e.target.value??""}}
            @input=${e=>{this._reason=e.composedPath()[0].value??this._reason}}></jh-input>
          <div class="row">
            <jh-button submit size="small" label=${this.confirm??this.label}
              appearance=${this.appearance==="danger"?"danger":"primary"}
              ?pending=${this._pending} ?disabled=${!this._reason.trim()}></jh-button>
            <jh-button appearance="tertiary" size="small" label="Cancel" @click=${this._cancel}></jh-button>
          </div>
          ${this._error?n`<jh-notification appearance="negative" type="alert" hide-dismiss-button>${this._error}</jh-notification>`:p}
        </form>`:p}`}};customElements.define("box-reason",Vt);var rt=window,it=rt.ShadowRoot&&(rt.ShadyCSS===void 0||rt.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,Zt=Symbol(),Fr=new WeakMap,Le=class{constructor(e,t,o){if(this._$cssResult$=!0,o!==Zt)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(it&&e===void 0){let o=t!==void 0&&t.length===1;o&&(e=Fr.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),o&&Fr.set(t,e))}return e}toString(){return this.cssText}},Kr=r=>new Le(typeof r=="string"?r:r+"",void 0,Zt),y=(r,...e)=>{let t=r.length===1?r[0]:e.reduce((o,i,a)=>o+(s=>{if(s._$cssResult$===!0)return s.cssText;if(typeof s=="number")return s;throw Error("Value passed to 'css' function must be a 'css' function result: "+s+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(i)+r[a+1],r[0]);return new Le(t,r,Zt)},Wt=(r,e)=>{it?r.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet):e.forEach(t=>{let o=document.createElement("style"),i=rt.litNonce;i!==void 0&&o.setAttribute("nonce",i),o.textContent=t.cssText,r.appendChild(o)})},at=it?r=>r:r=>r instanceof CSSStyleSheet?(e=>{let t="";for(let o of e.cssRules)t+=o.cssText;return Kr(t)})(r):r;var Ft,st=window,Yr=st.trustedTypes,ua=Yr?Yr.emptyScript:"",Gr=st.reactiveElementPolyfillSupport,Yt={toAttribute(r,e){switch(e){case Boolean:r=r?ua:null;break;case Object:case Array:r=r==null?r:JSON.stringify(r)}return r},fromAttribute(r,e){let t=r;switch(e){case Boolean:t=r!==null;break;case Number:t=r===null?null:Number(r);break;case Object:case Array:try{t=JSON.parse(r)}catch{t=null}}return t}},Qr=(r,e)=>e!==r&&(e==e||r==r),Kt={attribute:!0,type:String,converter:Yt,reflect:!1,hasChanged:Qr},Gt="finalized",B=class extends HTMLElement{constructor(){super(),this._$Ei=new Map,this.isUpdatePending=!1,this.hasUpdated=!1,this._$El=null,this._$Eu()}static addInitializer(e){var t;this.finalize(),((t=this.h)!==null&&t!==void 0?t:this.h=[]).push(e)}static get observedAttributes(){this.finalize();let e=[];return this.elementProperties.forEach((t,o)=>{let i=this._$Ep(o,t);i!==void 0&&(this._$Ev.set(i,o),e.push(i))}),e}static createProperty(e,t=Kt){if(t.state&&(t.attribute=!1),this.finalize(),this.elementProperties.set(e,t),!t.noAccessor&&!this.prototype.hasOwnProperty(e)){let o=typeof e=="symbol"?Symbol():"__"+e,i=this.getPropertyDescriptor(e,o,t);i!==void 0&&Object.defineProperty(this.prototype,e,i)}}static getPropertyDescriptor(e,t,o){return{get(){return this[t]},set(i){let a=this[e];this[t]=i,this.requestUpdate(e,a,o)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)||Kt}static finalize(){if(this.hasOwnProperty(Gt))return!1;this[Gt]=!0;let e=Object.getPrototypeOf(this);if(e.finalize(),e.h!==void 0&&(this.h=[...e.h]),this.elementProperties=new Map(e.elementProperties),this._$Ev=new Map,this.hasOwnProperty("properties")){let t=this.properties,o=[...Object.getOwnPropertyNames(t),...Object.getOwnPropertySymbols(t)];for(let i of o)this.createProperty(i,t[i])}return this.elementStyles=this.finalizeStyles(this.styles),!0}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let o=new Set(e.flat(1/0).reverse());for(let i of o)t.unshift(at(i))}else e!==void 0&&t.push(at(e));return t}static _$Ep(e,t){let o=t.attribute;return o===!1?void 0:typeof o=="string"?o:typeof e=="string"?e.toLowerCase():void 0}_$Eu(){var e;this._$E_=new Promise(t=>this.enableUpdating=t),this._$AL=new Map,this._$Eg(),this.requestUpdate(),(e=this.constructor.h)===null||e===void 0||e.forEach(t=>t(this))}addController(e){var t,o;((t=this._$ES)!==null&&t!==void 0?t:this._$ES=[]).push(e),this.renderRoot!==void 0&&this.isConnected&&((o=e.hostConnected)===null||o===void 0||o.call(e))}removeController(e){var t;(t=this._$ES)===null||t===void 0||t.splice(this._$ES.indexOf(e)>>>0,1)}_$Eg(){this.constructor.elementProperties.forEach((e,t)=>{this.hasOwnProperty(t)&&(this._$Ei.set(t,this[t]),delete this[t])})}createRenderRoot(){var e;let t=(e=this.shadowRoot)!==null&&e!==void 0?e:this.attachShadow(this.constructor.shadowRootOptions);return Wt(t,this.constructor.elementStyles),t}connectedCallback(){var e;this.renderRoot===void 0&&(this.renderRoot=this.createRenderRoot()),this.enableUpdating(!0),(e=this._$ES)===null||e===void 0||e.forEach(t=>{var o;return(o=t.hostConnected)===null||o===void 0?void 0:o.call(t)})}enableUpdating(e){}disconnectedCallback(){var e;(e=this._$ES)===null||e===void 0||e.forEach(t=>{var o;return(o=t.hostDisconnected)===null||o===void 0?void 0:o.call(t)})}attributeChangedCallback(e,t,o){this._$AK(e,o)}_$EO(e,t,o=Kt){var i;let a=this.constructor._$Ep(e,o);if(a!==void 0&&o.reflect===!0){let s=(((i=o.converter)===null||i===void 0?void 0:i.toAttribute)!==void 0?o.converter:Yt).toAttribute(t,o.type);this._$El=e,s==null?this.removeAttribute(a):this.setAttribute(a,s),this._$El=null}}_$AK(e,t){var o;let i=this.constructor,a=i._$Ev.get(e);if(a!==void 0&&this._$El!==a){let s=i.getPropertyOptions(a),h=typeof s.converter=="function"?{fromAttribute:s.converter}:((o=s.converter)===null||o===void 0?void 0:o.fromAttribute)!==void 0?s.converter:Yt;this._$El=a,this[a]=h.fromAttribute(t,s.type),this._$El=null}}requestUpdate(e,t,o){let i=!0;e!==void 0&&(((o=o||this.constructor.getPropertyOptions(e)).hasChanged||Qr)(this[e],t)?(this._$AL.has(e)||this._$AL.set(e,t),o.reflect===!0&&this._$El!==e&&(this._$EC===void 0&&(this._$EC=new Map),this._$EC.set(e,o))):i=!1),!this.isUpdatePending&&i&&(this._$E_=this._$Ej())}async _$Ej(){this.isUpdatePending=!0;try{await this._$E_}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){var e;if(!this.isUpdatePending)return;this.hasUpdated,this._$Ei&&(this._$Ei.forEach((i,a)=>this[a]=i),this._$Ei=void 0);let t=!1,o=this._$AL;try{t=this.shouldUpdate(o),t?(this.willUpdate(o),(e=this._$ES)===null||e===void 0||e.forEach(i=>{var a;return(a=i.hostUpdate)===null||a===void 0?void 0:a.call(i)}),this.update(o)):this._$Ek()}catch(i){throw t=!1,this._$Ek(),i}t&&this._$AE(o)}willUpdate(e){}_$AE(e){var t;(t=this._$ES)===null||t===void 0||t.forEach(o=>{var i;return(i=o.hostUpdated)===null||i===void 0?void 0:i.call(o)}),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$Ek(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$E_}shouldUpdate(e){return!0}update(e){this._$EC!==void 0&&(this._$EC.forEach((t,o)=>this._$EO(o,this[o],t)),this._$EC=void 0),this._$Ek()}updated(e){}firstUpdated(e){}};B[Gt]=!0,B.elementProperties=new Map,B.elementStyles=[],B.shadowRootOptions={mode:"open"},Gr?.({ReactiveElement:B}),((Ft=st.reactiveElementVersions)!==null&&Ft!==void 0?Ft:st.reactiveElementVersions=[]).push("1.6.3");var Qt,nt=window,ve=nt.trustedTypes,Xr=ve?ve.createPolicy("lit-html",{createHTML:r=>r}):void 0,Jt="$lit$",F=`lit$${(Math.random()+"").slice(9)}$`,ai="?"+F,ba=`<${ai}>`,ie=document,Pe=()=>ie.createComment(""),Oe=r=>r===null||typeof r!="object"&&typeof r!="function",si=Array.isArray,va=r=>si(r)||typeof r?.[Symbol.iterator]=="function",Xt=`[ 	
\f\r]`,Re=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,Jr=/-->/g,ei=/>/g,oe=RegExp(`>|${Xt}(?:([^\\s"'>=/]+)(${Xt}*=${Xt}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),ti=/'/g,oi=/"/g,ni=/^(?:script|style|textarea|title)$/i,li=r=>(e,...t)=>({_$litType$:r,strings:e,values:t}),d=li(1),js=li(2),ae=Symbol.for("lit-noChange"),x=Symbol.for("lit-nothing"),ri=new WeakMap,re=ie.createTreeWalker(ie,129,null,!1);function ci(r,e){if(!Array.isArray(r)||!r.hasOwnProperty("raw"))throw Error("invalid template strings array");return Xr!==void 0?Xr.createHTML(e):e}var ma=(r,e)=>{let t=r.length-1,o=[],i,a=e===2?"<svg>":"",s=Re;for(let h=0;h<t;h++){let l=r[h],c,b,u=-1,m=0;for(;m<l.length&&(s.lastIndex=m,b=s.exec(l),b!==null);)m=s.lastIndex,s===Re?b[1]==="!--"?s=Jr:b[1]!==void 0?s=ei:b[2]!==void 0?(ni.test(b[2])&&(i=RegExp("</"+b[2],"g")),s=oe):b[3]!==void 0&&(s=oe):s===oe?b[0]===">"?(s=i??Re,u=-1):b[1]===void 0?u=-2:(u=s.lastIndex-b[2].length,c=b[1],s=b[3]===void 0?oe:b[3]==='"'?oi:ti):s===oi||s===ti?s=oe:s===Jr||s===ei?s=Re:(s=oe,i=void 0);let f=s===oe&&r[h+1].startsWith("/>")?" ":"";a+=s===Re?l+ba:u>=0?(o.push(c),l.slice(0,u)+Jt+l.slice(u)+F+f):l+F+(u===-2?(o.push(void 0),h):f)}return[ci(r,a+(r[t]||"<?>")+(e===2?"</svg>":"")),o]},Ne=class r{constructor({strings:e,_$litType$:t},o){let i;this.parts=[];let a=0,s=0,h=e.length-1,l=this.parts,[c,b]=ma(e,t);if(this.el=r.createElement(c,o),re.currentNode=this.el.content,t===2){let u=this.el.content,m=u.firstChild;m.remove(),u.append(...m.childNodes)}for(;(i=re.nextNode())!==null&&l.length<h;){if(i.nodeType===1){if(i.hasAttributes()){let u=[];for(let m of i.getAttributeNames())if(m.endsWith(Jt)||m.startsWith(F)){let f=b[s++];if(u.push(m),f!==void 0){let L=i.getAttribute(f.toLowerCase()+Jt).split(F),T=/([.?@])?(.*)/.exec(f);l.push({type:1,index:a,name:T[2],strings:L,ctor:T[1]==="."?to:T[1]==="?"?oo:T[1]==="@"?ro:ge})}else l.push({type:6,index:a})}for(let m of u)i.removeAttribute(m)}if(ni.test(i.tagName)){let u=i.textContent.split(F),m=u.length-1;if(m>0){i.textContent=ve?ve.emptyScript:"";for(let f=0;f<m;f++)i.append(u[f],Pe()),re.nextNode(),l.push({type:2,index:++a});i.append(u[m],Pe())}}}else if(i.nodeType===8)if(i.data===ai)l.push({type:2,index:a});else{let u=-1;for(;(u=i.data.indexOf(F,u+1))!==-1;)l.push({type:7,index:a}),u+=F.length-1}a++}}static createElement(e,t){let o=ie.createElement("template");return o.innerHTML=e,o}};function me(r,e,t=r,o){var i,a,s,h;if(e===ae)return e;let l=o!==void 0?(i=t._$Co)===null||i===void 0?void 0:i[o]:t._$Cl,c=Oe(e)?void 0:e._$litDirective$;return l?.constructor!==c&&((a=l?._$AO)===null||a===void 0||a.call(l,!1),c===void 0?l=void 0:(l=new c(r),l._$AT(r,t,o)),o!==void 0?((s=(h=t)._$Co)!==null&&s!==void 0?s:h._$Co=[])[o]=l:t._$Cl=l),l!==void 0&&(e=me(r,l._$AS(r,e.values),l,o)),e}var eo=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){var t;let{el:{content:o},parts:i}=this._$AD,a=((t=e?.creationScope)!==null&&t!==void 0?t:ie).importNode(o,!0);re.currentNode=a;let s=re.nextNode(),h=0,l=0,c=i[0];for(;c!==void 0;){if(h===c.index){let b;c.type===2?b=new Ue(s,s.nextSibling,this,e):c.type===1?b=new c.ctor(s,c.name,c.strings,this,e):c.type===6&&(b=new io(s,this,e)),this._$AV.push(b),c=i[++l]}h!==c?.index&&(s=re.nextNode(),h++)}return re.currentNode=ie,a}v(e){let t=0;for(let o of this._$AV)o!==void 0&&(o.strings!==void 0?(o._$AI(e,o,t),t+=o.strings.length-2):o._$AI(e[t])),t++}},Ue=class r{constructor(e,t,o,i){var a;this.type=2,this._$AH=x,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=o,this.options=i,this._$Cp=(a=i?.isConnected)===null||a===void 0||a}get _$AU(){var e,t;return(t=(e=this._$AM)===null||e===void 0?void 0:e._$AU)!==null&&t!==void 0?t:this._$Cp}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=me(this,e,t),Oe(e)?e===x||e==null||e===""?(this._$AH!==x&&this._$AR(),this._$AH=x):e!==this._$AH&&e!==ae&&this._(e):e._$litType$!==void 0?this.g(e):e.nodeType!==void 0?this.$(e):va(e)?this.T(e):this._(e)}k(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}$(e){this._$AH!==e&&(this._$AR(),this._$AH=this.k(e))}_(e){this._$AH!==x&&Oe(this._$AH)?this._$AA.nextSibling.data=e:this.$(ie.createTextNode(e)),this._$AH=e}g(e){var t;let{values:o,_$litType$:i}=e,a=typeof i=="number"?this._$AC(e):(i.el===void 0&&(i.el=Ne.createElement(ci(i.h,i.h[0]),this.options)),i);if(((t=this._$AH)===null||t===void 0?void 0:t._$AD)===a)this._$AH.v(o);else{let s=new eo(a,this),h=s.u(this.options);s.v(o),this.$(h),this._$AH=s}}_$AC(e){let t=ri.get(e.strings);return t===void 0&&ri.set(e.strings,t=new Ne(e)),t}T(e){si(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,o,i=0;for(let a of e)i===t.length?t.push(o=new r(this.k(Pe()),this.k(Pe()),this,this.options)):o=t[i],o._$AI(a),i++;i<t.length&&(this._$AR(o&&o._$AB.nextSibling,i),t.length=i)}_$AR(e=this._$AA.nextSibling,t){var o;for((o=this._$AP)===null||o===void 0||o.call(this,!1,!0,t);e&&e!==this._$AB;){let i=e.nextSibling;e.remove(),e=i}}setConnected(e){var t;this._$AM===void 0&&(this._$Cp=e,(t=this._$AP)===null||t===void 0||t.call(this,e))}},ge=class{constructor(e,t,o,i,a){this.type=1,this._$AH=x,this._$AN=void 0,this.element=e,this.name=t,this._$AM=i,this.options=a,o.length>2||o[0]!==""||o[1]!==""?(this._$AH=Array(o.length-1).fill(new String),this.strings=o):this._$AH=x}get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}_$AI(e,t=this,o,i){let a=this.strings,s=!1;if(a===void 0)e=me(this,e,t,0),s=!Oe(e)||e!==this._$AH&&e!==ae,s&&(this._$AH=e);else{let h=e,l,c;for(e=a[0],l=0;l<a.length-1;l++)c=me(this,h[o+l],t,l),c===ae&&(c=this._$AH[l]),s||(s=!Oe(c)||c!==this._$AH[l]),c===x?e=x:e!==x&&(e+=(c??"")+a[l+1]),this._$AH[l]=c}s&&!i&&this.j(e)}j(e){e===x?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},to=class extends ge{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===x?void 0:e}},ga=ve?ve.emptyScript:"",oo=class extends ge{constructor(){super(...arguments),this.type=4}j(e){e&&e!==x?this.element.setAttribute(this.name,ga):this.element.removeAttribute(this.name)}},ro=class extends ge{constructor(e,t,o,i,a){super(e,t,o,i,a),this.type=5}_$AI(e,t=this){var o;if((e=(o=me(this,e,t,0))!==null&&o!==void 0?o:x)===ae)return;let i=this._$AH,a=e===x&&i!==x||e.capture!==i.capture||e.once!==i.once||e.passive!==i.passive,s=e!==x&&(i===x||a);a&&this.element.removeEventListener(this.name,this,i),s&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){var t,o;typeof this._$AH=="function"?this._$AH.call((o=(t=this.options)===null||t===void 0?void 0:t.host)!==null&&o!==void 0?o:this.element,e):this._$AH.handleEvent(e)}},io=class{constructor(e,t,o){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=o}get _$AU(){return this._$AM._$AU}_$AI(e){me(this,e)}};var ii=nt.litHtmlPolyfillSupport;ii?.(Ne,Ue),((Qt=nt.litHtmlVersions)!==null&&Qt!==void 0?Qt:nt.litHtmlVersions=[]).push("2.8.0");var hi=(r,e,t)=>{var o,i;let a=(o=t?.renderBefore)!==null&&o!==void 0?o:e,s=a._$litPart$;if(s===void 0){let h=(i=t?.renderBefore)!==null&&i!==void 0?i:null;a._$litPart$=s=new Ue(e.insertBefore(Pe(),h),h,void 0,t??{})}return s._$AI(r),s};var ao,so;var g=class extends B{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){var e,t;let o=super.createRenderRoot();return(e=(t=this.renderOptions).renderBefore)!==null&&e!==void 0||(t.renderBefore=o.firstChild),o}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=hi(t,this.renderRoot,this.renderOptions)}connectedCallback(){var e;super.connectedCallback(),(e=this._$Do)===null||e===void 0||e.setConnected(!0)}disconnectedCallback(){var e;super.disconnectedCallback(),(e=this._$Do)===null||e===void 0||e.setConnected(!1)}render(){return ae}};g.finalized=!0,g._$litElement$=!0,(ao=globalThis.litElementHydrateSupport)===null||ao===void 0||ao.call(globalThis,{LitElement:g});var di=globalThis.litElementPolyfillSupport;di?.({LitElement:g});((so=globalThis.litElementVersions)!==null&&so!==void 0?so:globalThis.litElementVersions=[]).push("3.3.3");var v=r=>r??x;var no=class extends g{#e;#t;static get styles(){return y`
      @keyframes indicator {
        from {
          margin-left: -45%;
        }
        to {
          margin-left: 95%;
        }
      }
      @keyframes spin {
        100% {
          transform: rotate(360deg);
        }
      }
      :host {
        font: var(--jh-font-helper-regular);
        display: block;
        width: 100%;
      }
      .text-content {
        color: var(
          --jh-progress-label-color,
          var(--jh-color-content-primary-enabled)
        );
        margin-bottom: var(--jh-space-100);
      }
      label {
        overflow-wrap: anywhere;
        word-break: normal;
      }
      span {
        color: var(
          --jh-progress-value-color,
          var(--jh-color-content-secondary-enabled)
        );
      }
      /* Linear */
      :host([label][type='linear']) .text-content {
        display: flex;
        justify-content: space-between;
      }
      :host([label=''][type='linear']) .text-content,
      :host(:not([label])[type='linear']) .text-content {
        display: flex;
        justify-content: flex-end;
      }
      span {
        width: auto;
      }
      span.has-label {
        margin-left: var(--jh-space-200);
      }
      .linear-progress-bar {
        background-color: var(
          --jh-progress-track-color,
          var(--jh-color-control-enabled)
        );
        border-radius: var(
          --jh-progress-track-border-radius,
          var(--jh-border-radius-50)
        );
        overflow: hidden;
      }
      :host([size='small']) .linear-progress-bar {
        height: var(--jh-size-50);
      }
      :host([size='medium']) .linear-progress-bar {
        height: var(--jh-size-100);
      }
      :host([size='large']) .linear-progress-bar {
        height: var(--jh-size-200);
      }
      :host .linear-progress-bar-value {
        background-color: var(
          --jh-progress-indicator-color,
          var(--jh-color-content-brand-enabled)
        );
        height: 100%;
      }
      /* Circular */
      :host([type='circular']) .container {
        display: flex;
        flex-direction: column-reverse;
        align-items: center;
      }
      :host([type='circular']) .text-content {
        margin-top: var(--jh-space-200);
        display: flex;
        flex-direction: column;
      }
      svg {
        transform: rotate(-90deg);
        width: var(--progress-size);
        height: var(--progress-size);
        overflow: visible;
      }
      :host([size='small']) svg {
        --progress-size: var(--jh-size-300);
      }
      :host([size='medium']) svg {
        --progress-size: var(--jh-size-400);
        margin-bottom: 1px;
      }
      :host([size='large']) svg {
        --progress-size: var(--jh-size-800);
        margin-bottom: 2px;
      }
      circle {
        transform-origin: 50% 50%;
        fill: none;
      }
      .circular-progress-bar {
        stroke: var(--jh-progress-track-color, var(--jh-color-control-enabled));
        stroke-dashoffset: 0;
      }
      .circular-progress-bar-value {
        stroke: var(
          --jh-progress-indicator-color,
          var(--jh-color-content-brand-enabled)
        );
      }
      :host([size='small']) circle {
        stroke-width: 1;
        stroke-dasharray: 37.8px;
      }
      :host([size='medium']) circle {
        stroke-width: 2;
        stroke-dasharray: 50.3px;
      }
      :host([size='large']) circle {
        stroke-width: 4;
        stroke-dasharray: 100.8px;
      }
      :host([type='circular']) div {
        text-align: center;
      }
      /* Indeterminate */
      :host([indeterminate]) .linear-progress-bar-value {
        width: 50%;
        animation: 1.2s cubic-bezier(0.65, 0, 0.35, 1) infinite alternate
          indicator;
      }
      :host([indeterminate]) svg {
        transform: none;
        animation: spin 0.75s linear infinite;
      }
    `}static get properties(){return{accessibleLabel:{type:String,attribute:"accessible-label"},accessibleValueText:{type:String,attribute:"accessible-valuetext"},hideValue:{type:Boolean,attribute:"hide-value"},indeterminate:{type:Boolean,reflect:!0},label:{type:String,reflect:!0},max:{type:Number},min:{type:Number},size:{type:String,reflect:!0},type:{type:String,reflect:!0},value:{type:Number}}}constructor(){super(),this.#e=this.attachInternals(),this.indeterminate=!1,this.label=null,this.size="medium",this.type="linear",this.hideValue=!1,this.#e.ariaLabel,this.#e.ariaValueMax,this.#e.ariaValueMin,this.#e.ariaValueNow,this.#e.ariaValueText,this.#e.role="progressbar",this.accessibleLabel=null,this.max=100,this.min=0,this.value=0,this.accessibleValueText=null}connectedCallback(){super.connectedCallback(),new MutationObserver(this.#o.bind(this)).observe(this,{attributeFilter:["indeterminate"]})}#o(){this.indeterminate&&(this.value=null)}#r(e,t){let o=this.#e[e];t!==o&&(this.#e[e]=t),this.requestUpdate(e,o)}get label(){return this.#t}set label(e){let t=this.label;e!==t&&(this.#t=e,this.accessibleLabel===null&&(this.accessibleLabel=e)),this.requestUpdate("label",t)}get accessibleLabel(){return this.#e.ariaLabel}set accessibleLabel(e){this.#r("ariaLabel",e)}get min(){return this.#e.ariaValueMin}set min(e){this.#r("ariaValueMin",e)}get max(){return this.#e.ariaValueMax}set max(e){this.#r("ariaValueMax",e)}get accessibleValueText(){return this.#e.ariaValueText}set accessibleValueText(e){this.#r("ariaValueText",e)}get value(){return this.#e.ariaValueNow}set value(e){this.#r("ariaValueNow",e)}#i(e,t,o){let i;t?i=75:o===null?i=0:i=o;let a={small:[6,6,37.8],medium:[8,8,50.3],large:[16,16,100.8]},[s,h,l]=a[e],c=l-l*Number(i)/100;return d`
      <svg>
        <circle
          class="circular-progress-bar"
          r=${s}
          cx=${h}
          cy=${h}
          c=${l}
        ></circle>
        <circle
          class="circular-progress-bar-value"
          r=${s}
          cx=${h}
          cy=${h}
          c=${l}
          style="stroke-dashoffset:${c}px"
        ></circle>
      </svg>
    `}#a(e){return d`
      <div class="linear-progress-bar">
        <div
          class="linear-progress-bar-value"
          style=${v(this.indeterminate?null:`width:${e||0}%`)}
        ></div>
      </div>
    `}render(){let e,t,o,i,a=Math.round((+this.value-+this.min)/(+this.max-+this.min)*100);return this.label&&(i=d`<label>${this.label}</label>`),this.type==="linear"?e=this.#a(a):this.type==="circular"&&(e=this.#i(this.size,this.indeterminate,a)),this.value&&!this.hideValue&&!this.indeterminate&&(this.label&&this.type==="linear"&&(o="has-label"),t=d`
        <span class=${v(o)}>${a+"%"}</span>
      `),d`
      <div class="container">
        ${this.label||this.value&&!this.hideValue?d`<div class="text-content">${i} ${t}</div>`:null}
        ${e}
      </div>
    `}};customElements.define("jh-progress",no);var lo=class extends g{static get formAssociated(){return!0}#e;#t;static get styles(){return y`
      :host {
        display: inline-block;
        touch-action: manipulation;
        vertical-align: middle;
      }
      :host(:not([block])) {
        /* Prevent expansion in grid layouts — override author styles */
        width: fit-content;
        /* Prevent flex-based stretching — override author styles */
        flex: 0 0 auto !important;
      }
      :host([block]) {
        display: block;
        text-align: center;
        /* Ensures button expands to fill its grid cell — overrides author alignment */
        justify-self: stretch !important;
      }
      :host(:not([label])[block]) button,
      :host(:not([label])[block]) a,
      :host([block]) button,
      :host([block]) a {
        width: 100%;
      }
      a {
        text-decoration: none;
        box-sizing: border-box;
      }
      button,
      a {
        border-radius: var(
          --jh-button-border-radius,
          var(--jh-border-radius-100)
        );
        font: var(--jh-font-body-medium-1);
        border: 1px solid var(--button-border-color);
        padding: 0 var(--jh-space-400);
        display: inline-flex;
        justify-content: center;
        align-items: center;
        position: relative;
        margin: 0;
        cursor: pointer;
        transition: background-color ease 0.2s;
      }
      .content-wrapper {
        display: flex;
        justify-content: center;
        align-items: center;
      }
      button:focus,
      a:focus {
        outline: 0;
      }
      button:focus-visible,
      a:focus-visible {
        box-shadow: var(--jh-button-shadow-focus, var(--jh-shadow-focus));
      }
      :host([pending]),
      :host([accessible-disabled='true']),
      :host([disabled]) button,
      :host([disabled]) a {
        cursor: default;
        pointer-events: none;
      }
      /* Size styling ('medium' is default) */
      :host([size='small']) button,
      :host([size='small']) a {
        height: var(--jh-button-size, var(--jh-size-1000));
      }
      :host([size='medium']) button,
      :host([size='medium']) a {
        height: var(--jh-button-size, var(--jh-size-1200));
      }
      :host([size='large']) button,
      :host([size='large']) a {
        height: var(--jh-button-size, var(--jh-size-1400));
      }
      /* appearance='primary' ('secondary' is default) */
      :host([appearance='primary']) button,
      :host([appearance='primary']) a {
        background-color: var(
          --jh-button-color-background-primary-enabled,
          var(--jh-color-content-brand-enabled)
        );
        color: var(
          --jh-button-label-color-text-primary-enabled,
          var(--jh-color-content-on-brand-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-primary-enabled,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-primary-enabled,
          var(--jh-color-content-on-brand-enabled)
        );
      }
      :host([appearance='primary']) button:focus-visible,
      :host([appearance='primary']) a:focus-visible {
        background-color: var(
          --jh-button-color-background-primary-focus,
          var(--jh-color-content-brand-hover)
        );
        color: var(
          --jh-button-label-color-text-primary-focus,
          var(--jh-color-content-on-brand-hover)
        );
        --button-border-color: var(
          --jh-button-color-border-primary-focus,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-primary-focus,
          var(--jh-color-content-on-brand-hover)
        );
      }
      :host([appearance='primary']) button:hover,
      :host([appearance='primary']) a:hover {
        background-color: var(
          --jh-button-color-background-primary-hover,
          var(--jh-color-content-brand-hover)
        );
        color: var(
          --jh-button-label-color-text-primary-hover,
          var(--jh-color-content-on-brand-hover)
        );
        --button-border-color: var(
          --jh-button-color-border-primary-hover,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-primary-hover,
          var(--jh-color-content-on-brand-hover)
        );
      }
      :host([appearance='primary']) button:active,
      :host([appearance='primary']) a:active {
        background-color: var(
          --jh-button-color-background-primary-active,
          var(--jh-color-content-brand-active)
        );
        color: var(
          --jh-button-label-color-text-primary-active,
          var(--jh-color-content-on-brand-active)
        );
        --button-border-color: var(
          --jh-button-color-border-primary-active,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-primary-active,
          var(--jh-color-content-on-brand-active)
        );
      }
      :host([appearance='primary'][accessible-disabled='true'][pending]) button,
      :host([appearance='primary']) button[aria-disabled='true'],
      :host([appearance='primary']) a[aria-disabled='true'],
      :host([appearance='primary'][disabled][pending]) button,
      :host([appearance='primary']) button[disabled],
      :host([appearance='primary']) a[disabled] {
        background-color: var(
          --jh-button-color-background-primary-disabled,
          var(--jh-color-content-brand-enabled)
        );
        color: var(
          --jh-button-label-color-text-primary-disabled,
          var(--jh-color-content-on-brand-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-primary-disabled,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-primary-disabled,
          var(--jh-color-content-on-brand-enabled)
        );
      }
      /* appearance='secondary' */
      :host([appearance='secondary']) button,
      :host([appearance='secondary']) a {
        background-color: var(
          --jh-button-color-background-secondary-enabled,
          transparent
        );
        color: var(
          --jh-button-label-color-text-secondary-enabled,
          var(--jh-color-content-brand-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-secondary-enabled,
          var(--jh-color-content-brand-enabled)
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-secondary-enabled,
          var(--jh-color-content-brand-enabled)
        );
      }
      :host([appearance='secondary']) button:focus-visible,
      :host([appearance='secondary']) a:focus-visible {
        background-color: var(
          --jh-button-color-background-secondary-focus,
          var(--jh-color-content-brand-hover)
        );
        color: var(
          --jh-button-label-color-text-secondary-focus,
          var(--jh-color-content-on-brand-hover)
        );
        --button-border-color: var(
          --jh-button-color-border-secondary-focus,
          var(--jh-color-content-brand-hover)
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-secondary-focus,
          var(--jh-color-content-on-brand-hover)
        );
      }
      :host([appearance='secondary']) button:hover,
      :host([appearance='secondary']) a:hover {
        background-color: var(
          --jh-button-color-background-secondary-hover,
          var(--jh-color-content-brand-hover)
        );
        color: var(
          --jh-button-label-color-text-secondary-hover,
          var(--jh-color-content-on-brand-hover)
        );
        --button-border-color: var(
          --jh-button-color-border-secondary-hover,
          var(--jh-color-content-brand-hover)
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-secondary-hover,
          var(--jh-color-content-on-brand-hover)
        );
      }
      :host([appearance='secondary']) button:active,
      :host([appearance='secondary']) a:active {
        background-color: var(
          --jh-button-color-background-secondary-active,
          var(--jh-color-content-brand-active)
        );
        color: var(
          --jh-button-label-color-text-secondary-active,
          var(--jh-color-content-on-brand-active)
        );
        --button-border-color: var(
          --jh-button-color-border-secondary-active,
          var(--jh-color-content-brand-active)
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-secondary-active,
          var(--jh-color-content-on-brand-active)
        );
      }
      :host([appearance='secondary'][accessible-disabled='true'][pending])
        button,
      :host([appearance='secondary']) button[aria-disabled='true'],
      :host([appearance='secondary']) a[aria-disabled='true'],
      :host([appearance='secondary'][disabled][pending]) button,
      :host([appearance='secondary']) button[disabled],
      :host([appearance='secondary']) a[disabled] {
        background-color: var(
          --jh-button-color-background-secondary-disabled,
          transparent
        );
        color: var(
          --jh-button-label-color-text-secondary-disabled,
          var(--jh-color-content-brand-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-secondary-disabled,
          var(--jh-color-content-brand-enabled)
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-secondary-disabled,
          var(--jh-color-content-brand-enabled)
        );
      }
      /* appearance='tertiary' */
      :host([appearance='tertiary']) button,
      :host([appearance='tertiary']) a {
        background-color: var(
          --jh-button-color-background-tertiary-enabled,
          transparent
        );
        color: var(
          --jh-button-label-color-text-tertiary-enabled,
          var(--jh-color-content-brand-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-tertiary-enabled,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-tertiary-enabled,
          var(--jh-color-content-brand-enabled)
        );
      }
      :host([appearance='tertiary']) button:focus-visible,
      :host([appearance='tertiary']) a:focus-visible {
        background-color: var(
          --jh-button-color-background-tertiary-focus,
          var(--jh-color-content-brand-hover)
        );
        color: var(
          --jh-button-label-color-text-tertiary-focus,
          var(--jh-color-content-on-brand-hover)
        );
        --button-border-color: var(
          --jh-button-color-border-tertiary-focus,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-tertiary-focus,
          var(--jh-color-content-on-brand-hover)
        );
      }
      :host([appearance='tertiary']) button:hover,
      :host([appearance='tertiary']) a:hover {
        background-color: var(
          --jh-button-color-background-tertiary-hover,
          var(--jh-color-content-brand-hover)
        );
        color: var(
          --jh-button-label-color-text-tertiary-hover,
          var(--jh-color-content-on-brand-hover)
        );
        --button-border-color: var(
          --jh-button-color-border-tertiary-hover,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-tertiary-hover,
          var(--jh-color-content-on-brand-hover)
        );
      }
      :host([appearance='tertiary']) button:active,
      :host([appearance='tertiary']) a:active {
        background-color: var(
          --jh-button-color-background-tertiary-active,
          var(--jh-color-content-brand-active)
        );
        color: var(
          --jh-button-label-color-text-tertiary-active,
          var(--jh-color-content-on-brand-active)
        );
        --button-border-color: var(
          --jh-button-color-border-tertiary-active,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-tertiary-active,
          var(--jh-color-content-on-brand-active)
        );
      }
      :host([appearance='tertiary'][accessible-disabled='true'][pending])
        button,
      :host([appearance='tertiary']) button[aria-disabled='true'],
      :host([appearance='tertiary']) a[aria-disabled='true'],
      :host([appearance='tertiary'][disabled][pending]) button,
      :host([appearance='tertiary']) button[disabled],
      :host([appearance='tertiary']) a[disabled] {
        background-color: var(
          --jh-button-color-background-tertiary-disabled,
          transparent
        );
        color: var(
          --jh-button-label-color-text-tertiary-disabled,
          var(--jh-color-content-brand-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-tertiary-disabled,
          transparent
        );
      }
      :host([appearance='tertiary']) button[aria-disabled='true'],
      :host([appearance='tertiary']) a[aria-disabled='true'],
      :host([appearance='tertiary']) button[disabled],
      :host([appearance='tertiary']) a[disabled] {
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-tertiary-disabled,
          var(--jh-color-content-brand-enabled)
        );
      }
      /* appearance='danger' */
      :host([appearance='danger']) button,
      :host([appearance='danger']) a {
        background-color: var(
          --jh-button-color-background-danger-enabled,
          var(--jh-color-content-negative-enabled)
        );
        color: var(
          --jh-button-label-color-text-danger-enabled,
          var(--jh-color-content-on-negative-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-danger-enabled,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-danger-enabled,
          var(--jh-color-content-on-negative-enabled)
        );
      }
      :host([appearance='danger']) button:focus-visible,
      :host([appearance='danger']) a:focus-visible {
        background-color: var(
          --jh-button-color-background-danger-focus,
          var(--jh-color-content-negative-hover)
        );
        color: var(
          --jh-button-label-color-text-danger-focus,
          var(--jh-color-content-on-negative-hover)
        );
        --button-border-color: var(
          --jh-button-color-border-danger-focus,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-danger-focus,
          var(--jh-color-content-on-negative-hover)
        );
      }
      :host([appearance='danger']) button:hover,
      :host([appearance='danger']) a:hover {
        background-color: var(
          --jh-button-color-background-danger-hover,
          var(--jh-color-content-negative-hover)
        );
        color: var(
          --jh-button-label-color-text-danger-hover,
          var(--jh-color-content-on-negative-hover)
        );
        --button-border-color: var(
          --jh-button-color-border-danger-hover,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-danger-hover,
          var(--jh-color-content-on-negative-hover)
        );
      }
      :host([appearance='danger']) button:active,
      :host([appearance='danger']) a:active {
        background-color: var(
          --jh-button-color-background-danger-active,
          var(--jh-color-content-negative-active)
        );
        color: var(
          --jh-button-label-color-text-danger-active,
          var(--jh-color-content-on-negative-active)
        );
        --button-border-color: var(
          --jh-button-color-border-danger-active,
          transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-danger-active,
          var(--jh-color-content-on-negative-active)
        );
      }
      :host([appearance='danger'][accessible-disabled='true'][pending]) button,
      :host([appearance='danger']) button[aria-disabled='true'],
      :host([appearance='danger']) a[aria-disabled='true'],
      :host([appearance='danger'][disabled][pending]) button,
      :host([appearance='danger']) button[disabled],
      :host([appearance='danger']) a[disabled] {
        background-color: var(
          --jh-button-color-background-danger-disabled,
          var(--jh-color-content-negative-enabled)
        );
        color: var(
          --jh-button-label-color-text-danger-disabled,
          var(--jh-color-content-on-negative-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-danger-disabled,
         transparent
        );
        --jh-icon-color-fill: var(
          --jh-button-icon-color-fill-danger-disabled,
          var(--jh-color-content-on-negative-enabled)
        );
      }
      /* additional disabled/aria-disabled/pending styling */
      :host([accessible-disabled='true'][pending]) button,
      button[aria-disabled='true'],
      a[aria-disabled='true'],
      :host([disabled][pending]) button,
      button[disabled],
      a[disabled] {
        opacity: var(--jh-button-opacity-disabled, var(--jh-opacity-disabled));
      }
      /* additional focus styling */
      :host([accessible-disabled='true']) button:focus-visible,
      :host([accessible-disabled='true']) a:focus-visible,
      :host([pending]) button:focus,
      :host([pending]) a:focus {
        box-shadow: none;
      }
      /* pending styling */
      :host([pending]) button,
      :host([pending]) a {
        width: var(--populated-button-width);
      }
      :host([pending][appearance='primary']) button,
      :host([pending][appearance='primary']) a {
        background-color: var(
          --jh-button-color-background-primary-pending,
          var(--jh-color-content-brand-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-primary-pending,
          transparent
        );
      }
      :host([pending][appearance='primary']) jh-progress {
        --jh-progress-indicator-color: var(
          --jh-button-progress-color-border-primary-pending,
          var(--jh-color-content-on-brand-enabled)
        );
      }
      :host([pending][appearance='secondary']) button,
      :host([pending][appearance='secondary']) a {
        background-color: var(
          --jh-button-color-background-secondary-pending,
          transparent
        );
        --button-border-color: var(
          --jh-button-color-border-secondary-pending,
          var(--jh-color-content-brand-enabled)
        );
      }
      :host([pending][appearance='secondary']) jh-progress {
        --jh-progress-indicator-color: var(
          --jh-button-progress-color-border-secondary-pending,
          var(--jh-color-content-brand-enabled)
        );
      }
      :host([pending][appearance='tertiary']) button,
      :host([pending][appearance='tertiary']) a {
        background-color: var(
          --jh-button-color-background-tertiary-pending,
          transparent
        );
        --button-border-color: var(
          --jh-button-color-border-tertiary-pending,
          transparent
        );
      }
      :host([pending][appearance='tertiary']) jh-progress {
        --jh-progress-indicator-color: var(
          --jh-button-progress-color-border-tertiary-pending,
          var(--jh-color-content-brand-enabled)
        );
      }
      :host([pending][appearance='danger']) button,
      :host([pending][appearance='danger']) a {
        background-color: var(
          --jh-button-color-background-danger-pending,
          var(--jh-color-content-negative-enabled)
        );
        --button-border-color: var(
          --jh-button-color-border-danger-pending,
          transparent
        );
      }
      :host([pending][appearance='danger']) jh-progress {
        --jh-progress-indicator-color: var(
          --jh-button-progress-color-border-danger-pending,
          var(--jh-color-content-on-negative-enabled)
        );
      }
      /* Icon-related Type option styling */
      :host([icon-position='before'][label]) ::slotted(*) {
        margin-right: var(--jh-space-200);
      }
      :host([icon-position='after'][label]) ::slotted(*) {
        margin-left: var(--jh-space-200);
      }
      :host([icon-position='after']) .content-wrapper {
        flex-direction: row-reverse;
      }
      /* Icon only styling */
      :host(:not([label]):not([block])[size='small']) button,
      :host(:not([label]):not([block])[size='small']) a {
        width: var(--jh-button-size, var(--jh-size-1000));
      }
      :host(:not([label]):not([block])[size='medium']) button,
      :host(:not([label]):not([block])[size='medium']) a {
        width: var(--jh-button-size, var(--jh-size-1200));
      }
      :host(:not([label]):not([block])[size='large']) button,
      :host(:not([label]):not([block])[size='large']) a {
        width: var(--jh-button-size, var(--jh-size-1400));
      }
      :host(:not([label])) button,
      :host(:not([label])) a {
        padding: 0;
      }
    `}static get properties(){return{accessibleDisabled:{type:String,attribute:"accessible-disabled",reflect:!0},accessibleLabel:{type:String,attribute:"accessible-label"},appearance:{type:String,reflect:!0},block:{type:Boolean},disabled:{type:Boolean,reflect:!0},href:{type:String},iconPosition:{type:String,attribute:"icon-position",reflect:!0},pending:{type:Boolean,reflect:!0},label:{type:String},name:{type:String},size:{type:String,reflect:!0},submit:{type:Boolean},target:{type:String},value:{type:String}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.form,this.accessibleDisabled=null,this.accessibleLabel=null,this.appearance="secondary",this.block=!1,this.disabled=!1,this.href=null,this.iconPosition="before",this.pending=!1,this.label=null,this.name=null,this.size="medium",this.submit=!1,this.target=null,this.value=null,this.addEventListener("click",this.#r),this.addEventListener("keydown",this.#i)}get value(){return this.#t}set value(e){let t=this.#t;e!==t&&(this.#t=e,this.#e.setFormValue(e)),this.requestUpdate("value",t)}firstUpdated(){this.#o(),new ResizeObserver(this.#o.bind(this)).observe(this)}#o(){let{width:e}=this.getBoundingClientRect();this.style.setProperty("--populated-button-width",`${e}px`)}formDisabledCallback(e){this.disabled=e}#r(e){this.submit&&this.#e.form&&!this.disabled&&this.#e.form.requestSubmit()}#i(e){(e.code==="Space"||e.code==="Enter")&&(this.accessibleDisabled==="true"||this.pending)&&e.preventDefault()}#a(){this.firstElementChild.setAttribute("aria-hidden","true"),this.firstElementChild.setAttribute("size","medium")}#s(e,t){let o,i;return t&&(i=d`<span>${t}</span>`),e&&!this.disabled?o=d`
        <jh-progress type="circular" indeterminate></jh-progress>
      `:o=d`
        <slot
          name="jh-button-icon"
          @slotchange=${this.#a}
        ></slot>
        ${i}
      `,d` <span class="content-wrapper">${o}</span> `}render(){let e=this.#s(this.pending,this.label),t;return this.accessibleDisabled!=="false"&&(t=this.accessibleDisabled),this.href?d`
        <a
          tabindex="0"
          aria-disabled=${v(t)}
          aria-label=${v(this.accessibleLabel)}
          ?disabled=${this.disabled}
          href=${v(this.disabled?null:this.href)}
          target=${v(this.disabled?null:this.target)}
        >
          ${e}
        </a>
      `:d`
        <button
          tabindex="0"
          aria-disabled=${v(t)}
          aria-label=${v(this.accessibleLabel)}
          ?disabled=${this.disabled}
          type=${this.submit?"submit":"button"}
        >
          ${e}
        </button>
      `}};customElements.define("jh-button",lo);var co=class extends g{static get styles(){return y`
        :host {
          border-bottom:
            var(--jh-divider-border-width, 1px)
            var(--jh-divider-border-style, solid)
            var(
              --jh-divider-color-border,
              var(--jh-color-divider-secondary)
            );
          display: block;
          margin-top: 16px;
          margin-bottom: 16px;
          box-sizing: content-box;
        }
        :host([inset='0']) {
          --inset: 0;
        }
        :host([inset='8']) {
          --inset: var(--jh-space-200);
        }
        :host([inset='16']) {
          --inset: var(--jh-space-400);
        }
        :host([inset='24']) {
          --inset: var(--jh-space-600);
        }
        :host([inset='32']) {
          --inset: var(--jh-space-800);
        }
        :host([inset='40']) {
          --inset: var(--jh-space-1000);
        }
        :host([inset='48']) {
          --inset: var(--jh-space-1200);
        }
        :host([inset='56']) {
          --inset: var(--jh-space-1400);
        }
        :host([inset='64']) {
          --inset: var(--jh-space-1600);
        }
        :host([inset='72']) {
          --inset: var(--jh-space-1800);
        }
        :host([inset='80']) {
          --inset: var(--jh-space-2000);
        }
        :host([inset='88']) {
          --inset: var(--jh-space-2200);
        }
        :host([inset='96']) {
          --inset: var(--jh-space-2400);
        }
        :host,
        :host[inset] {
          margin-left: var(--inset, var(--jh-divider-space-inset));
        }
      `}static get properties(){return{inset:{type:Number,reflect:!0}}}constructor(){super(),this.inset=null}};customElements.define("jh-divider",co);var ho=class extends g{static get styles(){return y`
      :host {
        --border-radius: var(--jh-border-radius-400);
        background-color: var(
          --jh-card-color-background,
          var(--jh-color-container-primary-enabled)
        );
        border-radius: var(
          --jh-card-border-radius,
          var(--border-radius)
        );
        box-shadow: var(--jh-card-shadow, var(--jh-shadow-100));
        word-break: break-word;
        display: block;
      }
      .display-slot {
        display: block;
      }
      slot[name='jh-card-media'] {
        padding: var(--jh-card-media-space-padding, 0);
        border-radius: var(--border-radius) var(--border-radius) 0 0;
        overflow: hidden;
      }
      ::slotted([slot='jh-card-media']) {
        aspect-ratio: var(--jh-card-media-aspect-ratio, auto);
        width: 100%;
      }
      ::slotted([slot='jh-card-header']) {
        color: var(
          --jh-card-header-color-text,
          var(--jh-color-content-primary-enabled)
        );
        font: var(--jh-font-heading-medium-3);
      }
      h1,
      h2,
      h3,
      h4,
      h5,
      h6 {
        color: var(
          --jh-card-header-title-color-text,
          var(--jh-color-content-primary-enabled)
        );
        font: var(--jh-font-heading-medium-3);
        margin: 0;
      }
      p {
        color: var(
          --jh-card-header-subtitle-color-text,
          var(--jh-color-content-secondary-enabled)
        );
        font: var(--jh-font-body-regular-1);
        margin: 0;
      }
      slot:not([name]) {
        color: var(
          --jh-card-default-color-text,
          var(--jh-color-content-primary-enabled)
        );
        font: var(--jh-font-body-regular-1);
      }
      ::slotted([slot='jh-card-footer']) {
        color: var(
          --jh-card-footer-color-text,
          var(--jh-color-content-secondary-enabled)
        );
        font: var(--jh-font-helper-regular);
      }
      .card-header-divider,
      .card-footer-divider {
        margin-top: 0px;
        margin-bottom: 0px;
      }
      /* padding */
      :host([padding='small']) .display-header,
      :host([padding='small']) slot[name='jh-card-header'] {
        padding: var(
          --jh-card-header-space-padding,
          var(--jh-space-400) var(--jh-space-400) 0
        );
      }
      :host([padding='none']) .display-header,
      :host([padding='medium']) .display-header,
      :host([padding='none']) slot[name='jh-card-header'],
      :host([padding='medium']) slot[name='jh-card-header'] {
        padding: var(
          --jh-card-header-space-padding,
          var(--jh-space-400) var(--jh-space-600) 0
        );
      }
      :host([padding='none']) slot:not([name]) {
        padding: var(--jh-card-default-space-padding, var(--jh-space-400) 0);
      }
      :host([padding='small']) slot:not([name]) {
        padding: var(--jh-card-default-space-padding, var(--jh-space-400));
      }
      :host([padding='medium']) slot:not([name]) {
        padding: var(
          --jh-card-default-space-padding,
          var(--jh-space-400) var(--jh-space-600)
        );
      }
      :host([padding='small']) slot[name='jh-card-footer'] {
        padding: var(
          --jh-card-footer-space-padding,
          0 var(--jh-space-400) var(--jh-space-400)
        );
      }
      :host([padding='none']) slot[name='jh-card-footer'],
      :host([padding='medium']) slot[name='jh-card-footer'] {
        padding: var(
          --jh-card-footer-space-padding,
          0 var(--jh-space-600) var(--jh-space-400)
        );
      }
      /* dividers and padding */
      :host([show-header-divider][padding='small']) .display-header,
      :host([show-header-divider][padding='small']) slot[name='jh-card-header'] {
        padding: var(--jh-card-header-space-padding, var(--jh-space-400));
      }
      :host([show-header-divider][padding='none']) .display-header,
      :host([show-header-divider][padding='medium']) .display-header,
      :host([show-header-divider][padding='none']) slot[name='jh-card-header'],
      :host([show-header-divider][padding='medium']) slot[name='jh-card-header'] {
        padding: var(
          --jh-card-header-space-padding,
          var(--jh-space-400) var(--jh-space-600)
        );
      }
      :host([show-footer-divider][padding='small']) slot[name='jh-card-footer'] {
        padding: var(--jh-card-footer-space-padding, var(--jh-space-400));
      }
      :host([show-footer-divider][padding='none']) slot[name='jh-card-footer'],
      :host([show-footer-divider][padding='medium']) slot[name='jh-card-footer'] {
        padding: var(
          --jh-card-footer-space-padding,
          var(--jh-space-400) var(--jh-space-600)
        );
      }
    `}static get properties(){return{footerDividerInset:{type:Number,attribute:"footer-divider-inset"},headerDividerInset:{type:Number,attribute:"header-divider-inset"},showFooterDivider:{type:Boolean,attribute:"show-footer-divider",reflect:!0},showHeaderDivider:{type:Boolean,attribute:"show-header-divider",reflect:!0},padding:{type:String,reflect:!0},headerSubtitle:{type:String,attribute:"header-subtitle"},headerTitle:{type:String,attribute:"header-title"},titleHeadingLevel:{type:Number,attribute:"title-heading-level"}}}constructor(){super(),this.footerDividerInset=null,this.headerDividerInset=null,this.titleHeadingLevel=2,this.padding="medium",this.showFooterDivider=!1,this.showHeaderDivider=!1,this.headerSubtitle=null,this.headerTitle=null}#e(){switch(this.titleHeadingLevel){case 1:return d`<h1>${this.headerTitle}</h1>`;case 2:return d`<h2>${this.headerTitle}</h2>`;case 3:return d`<h3>${this.headerTitle}</h3>`;case 4:return d`<h4>${this.headerTitle}</h4>`;case 5:return d`<h5>${this.headerTitle}</h5>`;case 6:return d`<h6>${this.headerTitle}</h6>`;default:return d`<h2>${this.headerTitle}</h2>`}}#t(e){e.target.classList.add("display-slot")}render(){let e,t,o;return(this.headerTitle||this.headerSubtitle)&&(e=d`
        <div class="display-header">
          ${this.headerTitle?this.#e():null}
          ${this.headerSubtitle?d`<p>${this.headerSubtitle}</p>`:null}
        </div>
      `),this.showHeaderDivider&&(t=d`
        <jh-divider
          class="card-header-divider"
          inset=${v(this.headerDividerInset?this.headerDividerInset:null)}
        >
        </jh-divider>
      `),this.showFooterDivider&&(o=d`
        <jh-divider
          class="card-footer-divider"
          inset=${v(this.footerDividerInset?this.footerDividerInset:null)}
        >
        </jh-divider>
      `),d`
      <slot name="jh-card-media" @slotchange=${this.#t}></slot>
      <slot name="jh-card-header" @slotchange=${this.#t}>${e}</slot>
      ${t}
      <slot @slotchange=${this.#t}></slot>
      ${o}
      <slot name="jh-card-footer" @slotchange=${this.#t}></slot>
    `}};customElements.define("jh-card",ho);var lt=window,ct=lt.ShadowRoot&&(lt.ShadyCSS===void 0||lt.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,po=Symbol(),pi=new WeakMap,Me=class{constructor(e,t,o){if(this._$cssResult$=!0,o!==po)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=e,this.t=t}get styleSheet(){let e=this.o,t=this.t;if(ct&&e===void 0){let o=t!==void 0&&t.length===1;o&&(e=pi.get(t)),e===void 0&&((this.o=e=new CSSStyleSheet).replaceSync(this.cssText),o&&pi.set(t,e))}return e}toString(){return this.cssText}},ui=r=>new Me(typeof r=="string"?r:r+"",void 0,po),P=(r,...e)=>{let t=r.length===1?r[0]:e.reduce((o,i,a)=>o+(s=>{if(s._$cssResult$===!0)return s.cssText;if(typeof s=="number")return s;throw Error("Value passed to 'css' function must be a 'css' function result: "+s+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(i)+r[a+1],r[0]);return new Me(t,r,po)},uo=(r,e)=>{ct?r.adoptedStyleSheets=e.map(t=>t instanceof CSSStyleSheet?t:t.styleSheet):e.forEach(t=>{let o=document.createElement("style"),i=lt.litNonce;i!==void 0&&o.setAttribute("nonce",i),o.textContent=t.cssText,r.appendChild(o)})},ht=ct?r=>r:r=>r instanceof CSSStyleSheet?(e=>{let t="";for(let o of e.cssRules)t+=o.cssText;return ui(t)})(r):r;var bo,dt=window,bi=dt.trustedTypes,fa=bi?bi.emptyScript:"",vi=dt.reactiveElementPolyfillSupport,mo={toAttribute(r,e){switch(e){case Boolean:r=r?fa:null;break;case Object:case Array:r=r==null?r:JSON.stringify(r)}return r},fromAttribute(r,e){let t=r;switch(e){case Boolean:t=r!==null;break;case Number:t=r===null?null:Number(r);break;case Object:case Array:try{t=JSON.parse(r)}catch{t=null}}return t}},mi=(r,e)=>e!==r&&(e==e||r==r),vo={attribute:!0,type:String,converter:mo,reflect:!1,hasChanged:mi},go="finalized",q=class extends HTMLElement{constructor(){super(),this._$Ei=new Map,this.isUpdatePending=!1,this.hasUpdated=!1,this._$El=null,this._$Eu()}static addInitializer(e){var t;this.finalize(),((t=this.h)!==null&&t!==void 0?t:this.h=[]).push(e)}static get observedAttributes(){this.finalize();let e=[];return this.elementProperties.forEach((t,o)=>{let i=this._$Ep(o,t);i!==void 0&&(this._$Ev.set(i,o),e.push(i))}),e}static createProperty(e,t=vo){if(t.state&&(t.attribute=!1),this.finalize(),this.elementProperties.set(e,t),!t.noAccessor&&!this.prototype.hasOwnProperty(e)){let o=typeof e=="symbol"?Symbol():"__"+e,i=this.getPropertyDescriptor(e,o,t);i!==void 0&&Object.defineProperty(this.prototype,e,i)}}static getPropertyDescriptor(e,t,o){return{get(){return this[t]},set(i){let a=this[e];this[t]=i,this.requestUpdate(e,a,o)},configurable:!0,enumerable:!0}}static getPropertyOptions(e){return this.elementProperties.get(e)||vo}static finalize(){if(this.hasOwnProperty(go))return!1;this[go]=!0;let e=Object.getPrototypeOf(this);if(e.finalize(),e.h!==void 0&&(this.h=[...e.h]),this.elementProperties=new Map(e.elementProperties),this._$Ev=new Map,this.hasOwnProperty("properties")){let t=this.properties,o=[...Object.getOwnPropertyNames(t),...Object.getOwnPropertySymbols(t)];for(let i of o)this.createProperty(i,t[i])}return this.elementStyles=this.finalizeStyles(this.styles),!0}static finalizeStyles(e){let t=[];if(Array.isArray(e)){let o=new Set(e.flat(1/0).reverse());for(let i of o)t.unshift(ht(i))}else e!==void 0&&t.push(ht(e));return t}static _$Ep(e,t){let o=t.attribute;return o===!1?void 0:typeof o=="string"?o:typeof e=="string"?e.toLowerCase():void 0}_$Eu(){var e;this._$E_=new Promise(t=>this.enableUpdating=t),this._$AL=new Map,this._$Eg(),this.requestUpdate(),(e=this.constructor.h)===null||e===void 0||e.forEach(t=>t(this))}addController(e){var t,o;((t=this._$ES)!==null&&t!==void 0?t:this._$ES=[]).push(e),this.renderRoot!==void 0&&this.isConnected&&((o=e.hostConnected)===null||o===void 0||o.call(e))}removeController(e){var t;(t=this._$ES)===null||t===void 0||t.splice(this._$ES.indexOf(e)>>>0,1)}_$Eg(){this.constructor.elementProperties.forEach((e,t)=>{this.hasOwnProperty(t)&&(this._$Ei.set(t,this[t]),delete this[t])})}createRenderRoot(){var e;let t=(e=this.shadowRoot)!==null&&e!==void 0?e:this.attachShadow(this.constructor.shadowRootOptions);return uo(t,this.constructor.elementStyles),t}connectedCallback(){var e;this.renderRoot===void 0&&(this.renderRoot=this.createRenderRoot()),this.enableUpdating(!0),(e=this._$ES)===null||e===void 0||e.forEach(t=>{var o;return(o=t.hostConnected)===null||o===void 0?void 0:o.call(t)})}enableUpdating(e){}disconnectedCallback(){var e;(e=this._$ES)===null||e===void 0||e.forEach(t=>{var o;return(o=t.hostDisconnected)===null||o===void 0?void 0:o.call(t)})}attributeChangedCallback(e,t,o){this._$AK(e,o)}_$EO(e,t,o=vo){var i;let a=this.constructor._$Ep(e,o);if(a!==void 0&&o.reflect===!0){let s=(((i=o.converter)===null||i===void 0?void 0:i.toAttribute)!==void 0?o.converter:mo).toAttribute(t,o.type);this._$El=e,s==null?this.removeAttribute(a):this.setAttribute(a,s),this._$El=null}}_$AK(e,t){var o;let i=this.constructor,a=i._$Ev.get(e);if(a!==void 0&&this._$El!==a){let s=i.getPropertyOptions(a),h=typeof s.converter=="function"?{fromAttribute:s.converter}:((o=s.converter)===null||o===void 0?void 0:o.fromAttribute)!==void 0?s.converter:mo;this._$El=a,this[a]=h.fromAttribute(t,s.type),this._$El=null}}requestUpdate(e,t,o){let i=!0;e!==void 0&&(((o=o||this.constructor.getPropertyOptions(e)).hasChanged||mi)(this[e],t)?(this._$AL.has(e)||this._$AL.set(e,t),o.reflect===!0&&this._$El!==e&&(this._$EC===void 0&&(this._$EC=new Map),this._$EC.set(e,o))):i=!1),!this.isUpdatePending&&i&&(this._$E_=this._$Ej())}async _$Ej(){this.isUpdatePending=!0;try{await this._$E_}catch(t){Promise.reject(t)}let e=this.scheduleUpdate();return e!=null&&await e,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){var e;if(!this.isUpdatePending)return;this.hasUpdated,this._$Ei&&(this._$Ei.forEach((i,a)=>this[a]=i),this._$Ei=void 0);let t=!1,o=this._$AL;try{t=this.shouldUpdate(o),t?(this.willUpdate(o),(e=this._$ES)===null||e===void 0||e.forEach(i=>{var a;return(a=i.hostUpdate)===null||a===void 0?void 0:a.call(i)}),this.update(o)):this._$Ek()}catch(i){throw t=!1,this._$Ek(),i}t&&this._$AE(o)}willUpdate(e){}_$AE(e){var t;(t=this._$ES)===null||t===void 0||t.forEach(o=>{var i;return(i=o.hostUpdated)===null||i===void 0?void 0:i.call(o)}),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(e)),this.updated(e)}_$Ek(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$E_}shouldUpdate(e){return!0}update(e){this._$EC!==void 0&&(this._$EC.forEach((t,o)=>this._$EO(o,this[o],t)),this._$EC=void 0),this._$Ek()}updated(e){}firstUpdated(e){}};q[go]=!0,q.elementProperties=new Map,q.elementStyles=[],q.shadowRootOptions={mode:"open"},vi?.({ReactiveElement:q}),((bo=dt.reactiveElementVersions)!==null&&bo!==void 0?bo:dt.reactiveElementVersions=[]).push("1.6.3");var fo,pt=window,fe=pt.trustedTypes,gi=fe?fe.createPolicy("lit-html",{createHTML:r=>r}):void 0,jo="$lit$",K=`lit$${(Math.random()+"").slice(9)}$`,wi="?"+K,ya=`<${wi}>`,le=document,He=()=>le.createComment(""),Ie=r=>r===null||typeof r!="object"&&typeof r!="function",ki=Array.isArray,ja=r=>ki(r)||typeof r?.[Symbol.iterator]=="function",yo=`[ 	
\f\r]`,De=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,fi=/-->/g,yi=/>/g,se=RegExp(`>|${yo}(?:([^\\s"'>=/]+)(${yo}*=${yo}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),ji=/'/g,$i=/"/g,Ai=/^(?:script|style|textarea|title)$/i,Si=r=>(e,...t)=>({_$litType$:r,strings:e,values:t}),U=Si(1),Xs=Si(2),ce=Symbol.for("lit-noChange"),A=Symbol.for("lit-nothing"),xi=new WeakMap,ne=le.createTreeWalker(le,129,null,!1);function Ei(r,e){if(!Array.isArray(r)||!r.hasOwnProperty("raw"))throw Error("invalid template strings array");return gi!==void 0?gi.createHTML(e):e}var $a=(r,e)=>{let t=r.length-1,o=[],i,a=e===2?"<svg>":"",s=De;for(let h=0;h<t;h++){let l=r[h],c,b,u=-1,m=0;for(;m<l.length&&(s.lastIndex=m,b=s.exec(l),b!==null);)m=s.lastIndex,s===De?b[1]==="!--"?s=fi:b[1]!==void 0?s=yi:b[2]!==void 0?(Ai.test(b[2])&&(i=RegExp("</"+b[2],"g")),s=se):b[3]!==void 0&&(s=se):s===se?b[0]===">"?(s=i??De,u=-1):b[1]===void 0?u=-2:(u=s.lastIndex-b[2].length,c=b[1],s=b[3]===void 0?se:b[3]==='"'?$i:ji):s===$i||s===ji?s=se:s===fi||s===yi?s=De:(s=se,i=void 0);let f=s===se&&r[h+1].startsWith("/>")?" ":"";a+=s===De?l+ya:u>=0?(o.push(c),l.slice(0,u)+jo+l.slice(u)+K+f):l+K+(u===-2?(o.push(void 0),h):f)}return[Ei(r,a+(r[t]||"<?>")+(e===2?"</svg>":"")),o]},Be=class r{constructor({strings:e,_$litType$:t},o){let i;this.parts=[];let a=0,s=0,h=e.length-1,l=this.parts,[c,b]=$a(e,t);if(this.el=r.createElement(c,o),ne.currentNode=this.el.content,t===2){let u=this.el.content,m=u.firstChild;m.remove(),u.append(...m.childNodes)}for(;(i=ne.nextNode())!==null&&l.length<h;){if(i.nodeType===1){if(i.hasAttributes()){let u=[];for(let m of i.getAttributeNames())if(m.endsWith(jo)||m.startsWith(K)){let f=b[s++];if(u.push(m),f!==void 0){let L=i.getAttribute(f.toLowerCase()+jo).split(K),T=/([.?@])?(.*)/.exec(f);l.push({type:1,index:a,name:T[2],strings:L,ctor:T[1]==="."?xo:T[1]==="?"?_o:T[1]==="@"?wo:je})}else l.push({type:6,index:a})}for(let m of u)i.removeAttribute(m)}if(Ai.test(i.tagName)){let u=i.textContent.split(K),m=u.length-1;if(m>0){i.textContent=fe?fe.emptyScript:"";for(let f=0;f<m;f++)i.append(u[f],He()),ne.nextNode(),l.push({type:2,index:++a});i.append(u[m],He())}}}else if(i.nodeType===8)if(i.data===wi)l.push({type:2,index:a});else{let u=-1;for(;(u=i.data.indexOf(K,u+1))!==-1;)l.push({type:7,index:a}),u+=K.length-1}a++}}static createElement(e,t){let o=le.createElement("template");return o.innerHTML=e,o}};function ye(r,e,t=r,o){var i,a,s,h;if(e===ce)return e;let l=o!==void 0?(i=t._$Co)===null||i===void 0?void 0:i[o]:t._$Cl,c=Ie(e)?void 0:e._$litDirective$;return l?.constructor!==c&&((a=l?._$AO)===null||a===void 0||a.call(l,!1),c===void 0?l=void 0:(l=new c(r),l._$AT(r,t,o)),o!==void 0?((s=(h=t)._$Co)!==null&&s!==void 0?s:h._$Co=[])[o]=l:t._$Cl=l),l!==void 0&&(e=ye(r,l._$AS(r,e.values),l,o)),e}var $o=class{constructor(e,t){this._$AV=[],this._$AN=void 0,this._$AD=e,this._$AM=t}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(e){var t;let{el:{content:o},parts:i}=this._$AD,a=((t=e?.creationScope)!==null&&t!==void 0?t:le).importNode(o,!0);ne.currentNode=a;let s=ne.nextNode(),h=0,l=0,c=i[0];for(;c!==void 0;){if(h===c.index){let b;c.type===2?b=new qe(s,s.nextSibling,this,e):c.type===1?b=new c.ctor(s,c.name,c.strings,this,e):c.type===6&&(b=new ko(s,this,e)),this._$AV.push(b),c=i[++l]}h!==c?.index&&(s=ne.nextNode(),h++)}return ne.currentNode=le,a}v(e){let t=0;for(let o of this._$AV)o!==void 0&&(o.strings!==void 0?(o._$AI(e,o,t),t+=o.strings.length-2):o._$AI(e[t])),t++}},qe=class r{constructor(e,t,o,i){var a;this.type=2,this._$AH=A,this._$AN=void 0,this._$AA=e,this._$AB=t,this._$AM=o,this.options=i,this._$Cp=(a=i?.isConnected)===null||a===void 0||a}get _$AU(){var e,t;return(t=(e=this._$AM)===null||e===void 0?void 0:e._$AU)!==null&&t!==void 0?t:this._$Cp}get parentNode(){let e=this._$AA.parentNode,t=this._$AM;return t!==void 0&&e?.nodeType===11&&(e=t.parentNode),e}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(e,t=this){e=ye(this,e,t),Ie(e)?e===A||e==null||e===""?(this._$AH!==A&&this._$AR(),this._$AH=A):e!==this._$AH&&e!==ce&&this._(e):e._$litType$!==void 0?this.g(e):e.nodeType!==void 0?this.$(e):ja(e)?this.T(e):this._(e)}k(e){return this._$AA.parentNode.insertBefore(e,this._$AB)}$(e){this._$AH!==e&&(this._$AR(),this._$AH=this.k(e))}_(e){this._$AH!==A&&Ie(this._$AH)?this._$AA.nextSibling.data=e:this.$(le.createTextNode(e)),this._$AH=e}g(e){var t;let{values:o,_$litType$:i}=e,a=typeof i=="number"?this._$AC(e):(i.el===void 0&&(i.el=Be.createElement(Ei(i.h,i.h[0]),this.options)),i);if(((t=this._$AH)===null||t===void 0?void 0:t._$AD)===a)this._$AH.v(o);else{let s=new $o(a,this),h=s.u(this.options);s.v(o),this.$(h),this._$AH=s}}_$AC(e){let t=xi.get(e.strings);return t===void 0&&xi.set(e.strings,t=new Be(e)),t}T(e){ki(this._$AH)||(this._$AH=[],this._$AR());let t=this._$AH,o,i=0;for(let a of e)i===t.length?t.push(o=new r(this.k(He()),this.k(He()),this,this.options)):o=t[i],o._$AI(a),i++;i<t.length&&(this._$AR(o&&o._$AB.nextSibling,i),t.length=i)}_$AR(e=this._$AA.nextSibling,t){var o;for((o=this._$AP)===null||o===void 0||o.call(this,!1,!0,t);e&&e!==this._$AB;){let i=e.nextSibling;e.remove(),e=i}}setConnected(e){var t;this._$AM===void 0&&(this._$Cp=e,(t=this._$AP)===null||t===void 0||t.call(this,e))}},je=class{constructor(e,t,o,i,a){this.type=1,this._$AH=A,this._$AN=void 0,this.element=e,this.name=t,this._$AM=i,this.options=a,o.length>2||o[0]!==""||o[1]!==""?(this._$AH=Array(o.length-1).fill(new String),this.strings=o):this._$AH=A}get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}_$AI(e,t=this,o,i){let a=this.strings,s=!1;if(a===void 0)e=ye(this,e,t,0),s=!Ie(e)||e!==this._$AH&&e!==ce,s&&(this._$AH=e);else{let h=e,l,c;for(e=a[0],l=0;l<a.length-1;l++)c=ye(this,h[o+l],t,l),c===ce&&(c=this._$AH[l]),s||(s=!Ie(c)||c!==this._$AH[l]),c===A?e=A:e!==A&&(e+=(c??"")+a[l+1]),this._$AH[l]=c}s&&!i&&this.j(e)}j(e){e===A?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,e??"")}},xo=class extends je{constructor(){super(...arguments),this.type=3}j(e){this.element[this.name]=e===A?void 0:e}},xa=fe?fe.emptyScript:"",_o=class extends je{constructor(){super(...arguments),this.type=4}j(e){e&&e!==A?this.element.setAttribute(this.name,xa):this.element.removeAttribute(this.name)}},wo=class extends je{constructor(e,t,o,i,a){super(e,t,o,i,a),this.type=5}_$AI(e,t=this){var o;if((e=(o=ye(this,e,t,0))!==null&&o!==void 0?o:A)===ce)return;let i=this._$AH,a=e===A&&i!==A||e.capture!==i.capture||e.once!==i.once||e.passive!==i.passive,s=e!==A&&(i===A||a);a&&this.element.removeEventListener(this.name,this,i),s&&this.element.addEventListener(this.name,this,e),this._$AH=e}handleEvent(e){var t,o;typeof this._$AH=="function"?this._$AH.call((o=(t=this.options)===null||t===void 0?void 0:t.host)!==null&&o!==void 0?o:this.element,e):this._$AH.handleEvent(e)}},ko=class{constructor(e,t,o){this.element=e,this.type=6,this._$AN=void 0,this._$AM=t,this.options=o}get _$AU(){return this._$AM._$AU}_$AI(e){ye(this,e)}};var _i=pt.litHtmlPolyfillSupport;_i?.(Be,qe),((fo=pt.litHtmlVersions)!==null&&fo!==void 0?fo:pt.litHtmlVersions=[]).push("2.8.0");var Ci=(r,e,t)=>{var o,i;let a=(o=t?.renderBefore)!==null&&o!==void 0?o:e,s=a._$litPart$;if(s===void 0){let h=(i=t?.renderBefore)!==null&&i!==void 0?i:null;a._$litPart$=s=new qe(e.insertBefore(He(),h),h,void 0,t??{})}return s._$AI(r),s};var Ao,So;var S=class extends q{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){var e,t;let o=super.createRenderRoot();return(e=(t=this.renderOptions).renderBefore)!==null&&e!==void 0||(t.renderBefore=o.firstChild),o}update(e){let t=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(e),this._$Do=Ci(t,this.renderRoot,this.renderOptions)}connectedCallback(){var e;super.connectedCallback(),(e=this._$Do)===null||e===void 0||e.setConnected(!0)}disconnectedCallback(){var e;super.disconnectedCallback(),(e=this._$Do)===null||e===void 0||e.setConnected(!1)}render(){return ce}};S.finalized=!0,S._$litElement$=!0,(Ao=globalThis.litElementHydrateSupport)===null||Ao===void 0||Ao.call(globalThis,{LitElement:S});var zi=globalThis.litElementPolyfillSupport;zi?.({LitElement:S});((So=globalThis.litElementVersions)!==null&&So!==void 0?So:globalThis.litElementVersions=[]).push("3.3.3");var ut=class extends S{#e;static get styles(){return P`
      :host {
        fill: var(
          --jh-icon-color-fill,
          var(--jh-color-content-secondary-enabled)
        );
        width: var(--icon-size);
        height: var(--icon-size);
        display: inline-block;
      }
      :host([size='x-small']) {
        --icon-size: var(
          --jh-icon-size-extra-small,
          var(--jh-size-400)
        );
      }
      :host([size='small']) {
        --icon-size: var(
          --jh-icon-size-small,
          var(--jh-size-500)
        );
      }
      :host([size='medium']) {
        --icon-size: var(
          --jh-icon-size-medium,
          var(--jh-size-600)
        );
      }
      :host([size='large']) {
        --icon-size: var(
          --jh-icon-size-large,
          var(--jh-size-900)
        );
      }
      :host([size='x-large']) {
        --icon-size: var(
          --jh-icon-size-extra-large,
          var(--jh-size-1400)
        );
      }
      svg {
        width: 100%;
        height: 100%;
      }
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="graphics-symbol",this.#e.ariaHidden="true",this.size="medium"}render(){return U`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M6.53 5.47a.75.75 0 0 0-1.06 1.06L10.94 12l-5.47 5.47a.75.75 0 0 0 1.06 1.06L12 13.06l5.47 5.47a.75.75 0 0 0 1.06-1.06L13.06 12l5.47-5.47a.75.75 0 1 0-1.06-1.06L12 10.94 6.53 5.47Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-xmark",ut);var _a=0,Ti="open",Eo=class extends g{#e;static get styles(){return y`
      :host {
        display: inline-block;
        position: relative;
      }
      :host span {
        background-color: var(
          --jh-tooltip-color-background,
          var(--jh-color-content-primary-enabled)
        );
        color: var(
          --jh-tooltip-color-text,
          var(--jh-color-content-on-primary-enabled)
        );
        border-radius: var(--jh-border-radius-100);
        padding: var(--jh-space-200);
        font: var(--jh-font-helper-regular);
        max-width: 160px;
        width: max-content;
        position: absolute;
        box-sizing: border-box;
        z-index: 1061;
        word-wrap: break-word;
        display: inline-block;
        transition: visibility cubic-bezier(0.1, 0.5, 0.1, 1) 0.09s;
        visibility: hidden;
        opacity: 0;
      }
      span::before {
        position: absolute;
        z-index: 1060;
        width: 0;
        height: 0;
        pointer-events: none;
        content: '';
        border: 4px solid transparent;
      }
      :host span.show {
        transition: opacity 0.3s cubic-bezier(0.1, 0.5, 0.1, 1) 0s;
        visibility: visible;
        opacity: 1;
      }

      /* padding and margin used to extend hover surface for tooltip persistence */
      :host([position='bottom-center']),
      :host([position='bottom-start']),
      :host([position='bottom-end']) {
        padding-bottom: 8px;
        margin-bottom: -8px;
      }
      :host([position='top-center']),
      :host([position='top-start']),
      :host([position='top-end']) {
        padding-top: 8px;
        margin-top: -8px;
      }
      :host([position='left']) {
        padding-left: 8px;
        margin-left: -8px;
      }
      :host([position='right']) {
        padding-right: 8px;
        margin-right: -8px;
      }

      /* placement of tooltip and arrow. Default is top-center */
      :host([position='bottom-center']) span,
      :host([position='bottom-start']) span,
      :host([position='bottom-end']) span {
        top: 100%;
        right: 50%;
      }
      :host([position='bottom-center']) span::before,
      :host([position='bottom-start']) span::before,
      :host([position='bottom-end']) span::before {
        border-bottom-color: var(
          --jh-tooltip-color-background,
          var(--jh-color-content-primary-enabled)
        );
        top: auto;
        bottom: 100%;
      }
      :host([position='bottom-center']) span::before {
        margin-right: calc(var(--jh-space-200) * -0.5);
        right: 50%;
      }
      :host([position='bottom-start']) span::before {
        left: var(--jh-space-200);
      }
      :host([position='bottom-end']) span::before {
        right: var(--jh-space-200);
      }
      :host([position='bottom-start']) span {
        margin-left: calc(var(--jh-space-200) * -1.5);
        right: auto;
        left: 50%;
      }
      :host([position='bottom-end']) span {
        margin-right: calc(var(--jh-space-200) * -1.5);
      }
      :host([position='top-center']) span,
      :host([position='top-start']) span,
      :host([position='top-end']) span {
        right: 50%;
        bottom: 100%;
      }
      :host([position='top-center']) span::before,
      :host([position='top-start']) span::before,
      :host([position='top-end']) span::before {
        border-top-color: var(
          --jh-tooltip-color-background,
          var(--jh-color-content-primary-enabled)
        );
        top: 100%;
        bottom: auto;
      }
      :host([position='top-center']) span::before {
        margin-right: calc(var(--jh-space-200) * -0.5);
        right: 50%;
      }
      :host([position='top-start']) span::before {
        left: var(--jh-space-200);
      }
      :host([position='top-end']) span::before {
        right: var(--jh-space-200);
      }
      :host([position='top-start']) span {
        margin-left: calc(var(--jh-space-200) * -1.5);
        right: auto;
        left: 50%;
      }
      :host([position='top-end']) span {
        margin-right: calc(var(--jh-space-200) * -1.5);
      }
      :host([position='bottom-center']) span,
      :host([position='top-center']) span {
        transform: translateX(50%);
      }
      :host([position='left']) span {
        right: 100%;
        bottom: 50%;
        transform: translateY(50%);
      }
      :host([position='left']) span::before {
        border-left-color: var(
          --jh-tooltip-color-background,
          var(--jh-color-content-primary-enabled)
        );
        margin-top: calc(var(--jh-space-200) * -0.5);
        top: 50%;
        bottom: 50%;
        left: 100%;
      }
      :host([position='right']) span {
        bottom: 50%;
        left: 100%;
        transform: translateY(50%);
      }
      :host([position='right']) span::before {
        border-right-color: var(
          --jh-tooltip-color-background,
          var(--jh-color-content-primary-enabled)
        );
        margin-top: calc(var(--jh-space-200) * -0.5);
        top: 50%;
        right: 100%;
        bottom: 50%;
      }
    `}static get properties(){return{flipDisabled:{type:Boolean,attribute:"flip-disabled"},label:{type:String},open:{type:Boolean,reflect:!0},position:{type:String,reflect:!0}}}constructor(){super(),this.#e=this.attachInternals(),this.flipDisabled=!1,this.label=null,this.open=!1,this.position="top-center"}connectedCallback(){super.connectedCallback(),this.id=`tooltip-describedby-${_a++}`;let e=new MutationObserver(this.#t.bind(this)),t={childList:!0};e.observe(this.shadowRoot,t)}#t(e){for(let t of e)Array.from(t.addedNodes).some(a=>a.tagName==="SPAN")&&(this.#e.role="tooltip",this.addEventListener("focus",this.#i,!0),this.addEventListener("mouseenter",this.#i),this.addEventListener("blur",this.#a,!0),this.addEventListener("mouseleave",this.#a),this.addEventListener("keydown",this.#r)),t.removedNodes.length>0&&(this.#e.role="",this.removeEventListener("focus",this.#i),this.removeEventListener("mouseenter",this.#i),this.removeEventListener("blur",this.#a),this.removeEventListener("mouseleave",this.#a),this.removeEventListener("keydown",this.#r))}disconnectedCallback(){super.disconnectedCallback(),this.removeEventListener("focus",this.#i),this.removeEventListener("mouseenter",this.#i),this.removeEventListener("blur",this.#a),this.removeEventListener("mouseleave",this.#a),this.removeEventListener("keydown",this.#r)}#o(){let e=this.getAttribute("id");this.firstElementChild.setAttribute("aria-describedby",e);let t=window.getComputedStyle(this.firstElementChild).display;this.style.setProperty("display",t)}updated(){this.open&&this.#s()}#r(e){e.key==="Escape"&&this.#a()}#i(){this.setAttribute(Ti,"")}#a(){this.removeAttribute(Ti)}#s(){let e=this.getAttribute("position");if(["left","right","top-start","top-end","top-center","bottom-start","bottom-end","bottom-center"].includes(e)&&this.flipDisabled===!1&&this.label){let[t,o]=e.split("-"),i=this.#c(),a=l=>e===l,s=l=>t===l.split("-")[0],h=l=>{let[c,b]=l.split("-");return e==="left"&&l==="right"||e==="right"&&l==="left"||t==="bottom"&&c==="top"&&o===b||t==="top"&&c==="bottom"&&o===b};if(!i.some(a)){let l=i.find(s)||i.find(h)||i[0];this.setAttribute("position",l)}}}#c(){let{tooltipWidth:e,tooltipHeight:t,elemHeight:o,elemWidth:i}=this.#h(),{elemLeft:a,elemRight:s,elemTop:h,elemBottom:l}=this.#n(),c=h-t>0,b=l-t>0,u=s-e>0,m=a-e>0,f=this.#l(h,o,t),L=this.#l(l,o,t),T=window.innerWidth-a-e>0,Xo=a>0,Jo=this.#l(s,i,e),er=this.#l(a,i,e),tr=s>0,or=a+i-e>0;return Object.entries({left:m&&f&&L,right:u&&f&&L,"top-start":c&&T&&Xo,"bottom-start":b&&T&&Xo,"top-center":c&&Jo&&er,"bottom-center":b&&Jo&&er,"top-end":c&&tr&&or,"bottom-end":b&&tr&&or}).reduce((rr,[Di,Hi])=>Hi?[...rr,Di]:rr,[])}#h(){return{tooltipWidth:this.shadowRoot.querySelector("span").getBoundingClientRect().width,tooltipHeight:this.shadowRoot.querySelector("span").getBoundingClientRect().height,elemHeight:this.getBoundingClientRect().height,elemWidth:this.getBoundingClientRect().width}}#n(){return{elemLeft:this.getBoundingClientRect().left,elemRight:window.innerWidth-this.getBoundingClientRect().right,elemTop:this.getBoundingClientRect().top,elemBottom:window.innerHeight-this.getBoundingClientRect().bottom}}#l(e,t,o){return e+t/2-o/2>0}render(){let e;return this.label&&(e=d`
        <span
          class=${v(this.open?"show":null)}
          aria-hidden=${this.open?"false":"true"}
        >
          ${this.label}
        </span>
      `),d`
      <slot @slotchange=${this.#o}></slot>
      ${e}
    `}};customElements.define("jh-tooltip",Eo);var Co=class extends g{#e=!1;#t;static get styles(){return y`
      :host {
        display: inline-block;
        vertical-align: middle;
        min-width: 0;
      }
      .container {
        background: var(
          --jh-tag-color-background-enabled,
          var(--jh-color-container-neutral-enabled)
        );
        color: var(
          --jh-tag-color-text-enabled,
          var(--jh-color-content-secondary-enabled)
        );
        border-color: var(--jh-tag-color-border-enabled, transparent);
        border-radius: var(
          --jh-tag-border-radius,
          var(--jh-border-radius-pill)
        );
        max-width: var(--jh-tag-size-max-width, 200px);
        font: var(--jh-font-helper-bold);
        column-gap: var(--jh-space-100);
        padding-left: var(--jh-space-200);
        padding-right: var(--jh-space-200);
        box-sizing: border-box;
        height: 100%;
        display: flex;
        justify-content: center;
        align-items: center;
        border-style: solid;
        border-width: 1px;
      }
      .label-container {
        display: flex;
        flex-direction: column;
        flex: 1 1 auto;
        min-width: 0;
      }
      a:link,
      a:visited,
      a:hover,
      a:active {
        text-decoration: none;
        color: inherit;
      }
      jh-button {
        --jh-button-border-radius: var(--jh-border-radius-circle);
        --jh-button-color-background-secondary-enabled: var(
          --jh-tag-dismiss-color-background-enabled,
          var(--jh-color-container-neutral-enabled)
        );
        --jh-button-color-background-secondary-focus: var(
          --jh-tag-dismiss-color-background-focus,
          var(--jh-color-container-neutral-hover)
        );
        --jh-button-color-background-secondary-hover: var(
          --jh-tag-dismiss-color-background-hover,
          var(--jh-color-container-neutral-hover)
        );
        --jh-button-color-background-secondary-active: var(
          --jh-tag-dismiss-color-background-active,
          var(--jh-color-container-neutral-active)
        );
        --jh-button-color-background-secondary-disabled: var(
          --jh-tag-dismiss-color-background-disabled,
          var(--jh-color-container-neutral-enabled)
        );
        --jh-button-color-border-secondary-enabled: transparent;
        --jh-button-color-border-secondary-focus: transparent;
        --jh-button-color-border-secondary-hover: transparent;
        --jh-button-color-border-secondary-active: transparent;
        --jh-button-color-border-secondary-disabled: transparent;
        --jh-button-icon-color-fill-secondary-enabled: var(
          --jh-tag-dismiss-icon-color-fill-enabled,
          var(--jh-color-content-secondary-enabled)
        );
        --jh-button-icon-color-fill-secondary-focus: var(
          --jh-tag-dismiss-icon-color-fill-focus,
          var(--jh-color-content-secondary-hover)
        );
        --jh-button-icon-color-fill-secondary-hover: var(
          --jh-tag-dismiss-icon-color-fill-hover,
          var(--jh-color-content-secondary-hover)
        );
        --jh-button-icon-color-fill-secondary-active: var(
          --jh-tag-dismiss-icon-color-fill-active,
          var(--jh-color-content-secondary-active)
        );
        --jh-button-icon-color-fill-secondary-disabled: var(
          --jh-tag-dismiss-icon-color-fill-disabled,
          var(--jh-color-content-secondary-enabled)
        );
        --jh-icon-size-medium: var(--jh-size-400);
        --jh-button-size: 18px;
        align-self: self-start;
      }
      slot[name='jh-tag-icon']::slotted(*) {
        padding: 1px 0;
        flex: 0 0 auto;
      }
      /* sizes */
      :host([size='small']) {
        height: var(--jh-size-500);
      }
      :host([size='medium']) {
        height: var(--jh-size-600);
      }
      :host([size='medium']) jh-button {
        padding: var(--jh-space-50) var(--jh-space-0);;
        padding-right: var(--jh-space-50);
      }

      /* vertical align of button with tooltip */
      jh-tooltip:has(jh-button) {
        align-self: self-start;
      }
      /* padding adjustments */
      .container:has(jh-button) {
        padding-right: var(--jh-space-0);
      }
      .container:has(.label:empty) {
        column-gap: var(--jh-space-50);
      }

      /* ellipsis truncation */
      .label {
        display: block;
        overflow: hidden;
        white-space: nowrap;
        text-overflow: ellipsis;
        padding: 2px 0;
      }

      :host([tabindex]:hover) .container,
      :host([href]:hover) .container {
        background: var(
          --jh-tag-color-background-hover,
          var(--jh-color-container-neutral-hover)
        );
        border-color: var(--jh-tag-color-border-hover, transparent);
        color: var(
          --jh-tag-color-text-hover,
          var(--jh-color-content-secondary-hover)
        );
        cursor: pointer;
      }

      /* focus ring */
      :host([tabindex]:focus-visible) .container,
      .container:has(a:focus-visible) {
        box-shadow: var(--jh-tag-shadow-focus, var(--jh-shadow-focus));
        background: var(
          --jh-tag-color-background-focus,
          var(--jh-color-container-neutral-hover)
        );
        color: var(
          --jh-tag-color-text-focus,
          var(--jh-color-content-secondary-hover)
        );
        border-color: var(--jh-tag-color-border-focus, transparent);
      }

      :host([tabindex]:active) .container,
      :host([href]:active) .container {
        background: var(
          --jh-tag-color-background-active,
          var(--jh-color-container-neutral-active)
        );
        border-color: var(--jh-tag-color-border-active, transparent);
        color: var(
          --jh-tag-color-text-active,
          var(--jh-color-content-secondary-active)
        );
      }
      :host([tabindex]:focus-visible),
      a:focus-visible {
        outline: none;
      }

      /* remove state styles from host when button has a state */
      :host([tabindex]:hover) .container:has(jh-button:hover),
      :host([href]:hover) .container:has(jh-button:hover),
      :host([tabindex]:active) .container:has(jh-button:active),
      :host([href]:active) .container:has(jh-button:active) {
        background: var(
          --jh-tag-color-background-enabled,
          var(--jh-color-container-neutral-enabled)
        );
        color: var(
          --jh-tag-color-text-enabled,
          var(--jh-color-content-secondary-enabled)
        );
        border-color: var(--jh-tag-color-border-enabled, transparent);
      }

      slot[name='jh-tag-icon'],
      :host([tabindex]:hover)
        .container:has(jh-button:hover)
        slot[name='jh-tag-icon'],
      :host([href]:hover)
        .container:has(jh-button:hover)
        slot[name='jh-tag-icon'],
        slot[name='jh-tag-icon'],
      :host([tabindex]:active)
        .container:has(jh-button:active)
        slot[name='jh-tag-icon'],
      :host([href]:active)
        .container:has(jh-button:active)
        slot[name='jh-tag-icon'] {
        --jh-icon-color-fill: var(
          --jh-tag-icon-color-fill-enabled,
          var(--jh-color-content-secondary-enabled)
        );
      }
      :host([tabindex]:hover) slot[name='jh-tag-icon'],
      :host([href]:hover) slot[name='jh-tag-icon'] {
        --jh-icon-color-fill: var(
          --jh-tag-icon-color-fill-hover,
          var(--jh-color-content-secondary-hover)
        );
      }
      :host([tabindex]:active) slot[name='jh-tag-icon'],
      :host([href]:active) slot[name='jh-tag-icon'] {
        --jh-icon-color-fill: var(
          --jh-tag-icon-color-fill-active,
          var(--jh-color-content-secondary-active)
        );
      }
      :host([tabindex]:focus-visible) slot[name='jh-tag-icon'],
      :host([href]:focus-visible) slot[name='jh-tag-icon'] {
        --jh-icon-color-fill: var(
          --jh-tag-icon-color-fill-focus,
          var(--jh-color-content-secondary-hover)
        );
      }
    `}static get properties(){return{label:{type:String},dismissButtonAccessibleLabel:{type:String,attribute:"dismiss-button-accessible-label"},dismissible:{type:Boolean},tooltipLabel:{type:String,attribute:"tooltip-label"},size:{type:String,reflect:!0},href:{type:String},target:{type:String},removeOnDismiss:{type:Boolean,attribute:"remove-on-dismiss"}}}constructor(){super(),this.label=null,this.dismissible=!1,this.dismissButtonAccessibleLabel=null,this.tooltipLabel=null,this.size="small",this.href=null,this.target=null,this.removeOnDismiss=!1,this.addEventListener("keyup",this.#i),this.addEventListener("keyup",this.#o),this.addEventListener("blur",this.#a)}connectedCallback(){super.connectedCallback(),new ResizeObserver(this.#r.bind(this)).observe(this)}#o(e){(e.code==="Space"||e.code==="Enter")&&this.click()}#r(){if(!this.label)return;let e=this.#e,t=this.shadowRoot.querySelector(".label");this.#e=t.offsetWidth<t.scrollWidth,e!==this.#e&&(this.#e&&!this.href?(this.#t=this.getAttribute("tabindex"),this.setAttribute("tabindex","0")):this.#e||(this.#t?this.setAttribute("tabindex",this.#t):this.removeAttribute("tabindex")),this.update())}#i(e){if(e.target.#e&&(e.key==="Tab"||e.shiftKey&&e.key==="Tab")){let t=this.shadowRoot.querySelector("jh-tooltip");(e.composedPath()[0].localName==="jh-tag"||e.composedPath()[0].localName==="a")&&(t.open=!t.open),e.composedPath()[0].localName==="button"&&(t.open=!1)}}#a(e){if(e.target.#e){let t=this.shadowRoot.querySelector("jh-tooltip");t.open=!1}}#s(e){e&&(this.shadowRoot.querySelector('slot[name="jh-tag-icon"]').classList.add("display-icon-slot"),this.firstElementChild.setAttribute("size","x-small"))}#c(e){e.stopPropagation(),this.dispatchEvent(new CustomEvent("jh-dismiss",{bubbles:!0,cancelable:!0,composed:!0})),this.removeOnDismiss&&this.remove()}#h(){let e;return this.dismissible&&(e=d`
      <jh-button
        size="medium"
        appearance="secondary"
        accessible-label=${this.dismissButtonAccessibleLabel}
        @click=${this.#c}>
        <slot name="jh-tag-dismiss-icon" slot="jh-button-icon">
          <jh-icon-xmark slot="jh-button-icon"></jh-icon-xmark>
        </slot>
      </jh-button>
      `),this.dismissible&&this.tooltipLabel&&(e=d`<jh-tooltip label=${this.tooltipLabel}>${e}</jh-tooltip>`),e}#n(){let e,t=d`<span class="label">${this.label}</span>`;return this.#e&&(t=d`<jh-tooltip label=${this.label}>${t}</jh-tooltip>`),this.href?(this.removeAttribute("tabindex"),e=d` <a class="label-container" tabindex="0" href=${this.href}>${t}</a>`):e=d` <div class="label-container">${t}</div>`,e}render(){return d` 
    <span class="container">
      <slot name="jh-tag-icon" @slotchange=${this.#s}></slot>
      ${this.#n()}
      ${this.#h()}
    </span>
    `}};customElements.define("jh-tag",Co);var zo=class extends g{#e;static get styles(){return y`
      :host {
        gap: var(--jh-space-100);
        display: flex;
        flex-wrap: wrap;
        align-content: flex-start;
      }
      :host([alignment='start']) {
        justify-content: flex-start;
      }
      :host([alignment='end']) {
        justify-content: flex-end;
      }
    `}static get properties(){return{alignment:{type:String,reflect:!0}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="group",this.alignment="start"}render(){return d` <slot></slot> `}};customElements.define("jh-tag-group",zo);var To=class extends g{#e;static get styles(){return y`
      :host {
        fill: var(
          --jh-icon-color-fill,
          var(--jh-color-content-secondary-enabled)
        );
        width: var(--icon-size);
        height: var(--icon-size);
        display: inline-block;
      }
      :host([size='x-small']) {
        --icon-size: var(
          --jh-icon-size-extra-small,
          var(--jh-size-400)
        );
      }
      :host([size='small']) {
        --icon-size: var(
          --jh-icon-size-small,
          var(--jh-size-500)
        );
      }
      :host([size='medium']) {
        --icon-size: var(
          --jh-icon-size-medium,
          var(--jh-size-600)
        );
      }
      :host([size='large']) {
        --icon-size: var(
          --jh-icon-size-large,
          var(--jh-size-900)
        );
      }
      :host([size='x-large']) {
        --icon-size: var(
          --jh-icon-size-extra-large,
          var(--jh-size-1400)
        );
      }
      svg,
      ::slotted(*) {
        width: 100%;
        height: 100%;
      }
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="graphics-symbol",this.#e.ariaHidden="true",this.size="medium"}render(){return d`
      <slot></slot>
    `}};customElements.define("jh-icon",To);var bt=class extends S{#e;static get styles(){return P`
      :host {
        fill: var(
          --jh-icon-color-fill,
          var(--jh-color-content-secondary-enabled)
        );
        width: var(--icon-size);
        height: var(--icon-size);
        display: inline-block;
      }
      :host([size='x-small']) {
        --icon-size: var(
          --jh-icon-size-extra-small,
          var(--jh-size-400)
        );
      }
      :host([size='small']) {
        --icon-size: var(
          --jh-icon-size-small,
          var(--jh-size-500)
        );
      }
      :host([size='medium']) {
        --icon-size: var(
          --jh-icon-size-medium,
          var(--jh-size-600)
        );
      }
      :host([size='large']) {
        --icon-size: var(
          --jh-icon-size-large,
          var(--jh-size-900)
        );
      }
      :host([size='x-large']) {
        --icon-size: var(
          --jh-icon-size-extra-large,
          var(--jh-size-1400)
        );
      }
      svg {
        width: 100%;
        height: 100%;
      }
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="graphics-symbol",this.#e.ariaHidden="true",this.size="medium"}render(){return U`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M4.625 10.375a5.75 5.75 0 1 1 11.5 0 5.75 5.75 0 0 1-11.5 0Zm5.75-7.25a7.25 7.25 0 1 0 4.569 12.88l4.65 4.65a.75.75 0 1 0 1.061-1.06l-4.65-4.651a7.25 7.25 0 0 0-5.63-11.819Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-magnifying-glass",bt);var vt=class extends S{#e;static get styles(){return P`
      :host {
        fill: var(
          --jh-icon-color-fill,
          var(--jh-color-content-secondary-enabled)
        );
        width: var(--icon-size);
        height: var(--icon-size);
        display: inline-block;
      }
      :host([size='x-small']) {
        --icon-size: var(
          --jh-icon-size-extra-small,
          var(--jh-size-400)
        );
      }
      :host([size='small']) {
        --icon-size: var(
          --jh-icon-size-small,
          var(--jh-size-500)
        );
      }
      :host([size='medium']) {
        --icon-size: var(
          --jh-icon-size-medium,
          var(--jh-size-600)
        );
      }
      :host([size='large']) {
        --icon-size: var(
          --jh-icon-size-large,
          var(--jh-size-900)
        );
      }
      :host([size='x-large']) {
        --icon-size: var(
          --jh-icon-size-extra-large,
          var(--jh-size-1400)
        );
      }
      svg {
        width: 100%;
        height: 100%;
      }
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="graphics-symbol",this.#e.ariaHidden="true",this.size="medium"}render(){return U`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M12 3.75a8.25 8.25 0 1 0 0 16.5 8.25 8.25 0 0 0 0-16.5ZM2.25 12c0-5.384 4.365-9.75 9.75-9.75s9.75 4.366 9.75 9.75c0 5.385-4.365 9.75-9.75 9.75S2.25 17.386 2.25 12Zm6.22-3.53a.75.75 0 0 1 1.06 0L12 10.94l2.47-2.47a.75.75 0 1 1 1.06 1.06L13.06 12l2.47 2.47a.75.75 0 1 1-1.06 1.06L12 13.062l-2.47 2.47a.75.75 0 0 1-1.06-1.06L10.94 12 8.47 9.53a.75.75 0 0 1 0-1.06Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-circle-xmark",vt);var mt=class extends S{#e;static get styles(){return P`
      :host {
        fill: var(
          --jh-icon-color-fill,
          var(--jh-color-content-secondary-enabled)
        );
        width: var(--icon-size);
        height: var(--icon-size);
        display: inline-block;
      }
      :host([size='x-small']) {
        --icon-size: var(
          --jh-icon-size-extra-small,
          var(--jh-size-400)
        );
      }
      :host([size='small']) {
        --icon-size: var(
          --jh-icon-size-small,
          var(--jh-size-500)
        );
      }
      :host([size='medium']) {
        --icon-size: var(
          --jh-icon-size-medium,
          var(--jh-size-600)
        );
      }
      :host([size='large']) {
        --icon-size: var(
          --jh-icon-size-large,
          var(--jh-size-900)
        );
      }
      :host([size='x-large']) {
        --icon-size: var(
          --jh-icon-size-extra-large,
          var(--jh-size-1400)
        );
      }
      svg {
        width: 100%;
        height: 100%;
      }
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="graphics-symbol",this.#e.ariaHidden="true",this.size="medium"}render(){return U`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M12 6.25c-4.354 0-6.76 2.428-9.066 4.754l-.464.466a.75.75 0 0 0 0 1.061l.464.467C5.24 15.324 7.646 17.751 12 17.751c4.354 0 6.76-2.427 9.066-4.753l.464-.467a.75.75 0 0 0 0-1.06l-.464-.467C18.76 8.678 16.354 6.25 12 6.25Zm0 10c-3.65 0-5.635-1.932-7.941-4.25C6.365 9.685 8.349 7.75 12 7.75c3.65 0 5.635 1.934 7.941 4.25-2.306 2.318-4.29 4.25-7.941 4.25ZM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-eye",mt);var gt=class extends S{#e;static get styles(){return P`
      :host {
        fill: var(
          --jh-icon-color-fill,
          var(--jh-color-content-secondary-enabled)
        );
        width: var(--icon-size);
        height: var(--icon-size);
        display: inline-block;
      }
      :host([size='x-small']) {
        --icon-size: var(
          --jh-icon-size-extra-small,
          var(--jh-size-400)
        );
      }
      :host([size='small']) {
        --icon-size: var(
          --jh-icon-size-small,
          var(--jh-size-500)
        );
      }
      :host([size='medium']) {
        --icon-size: var(
          --jh-icon-size-medium,
          var(--jh-size-600)
        );
      }
      :host([size='large']) {
        --icon-size: var(
          --jh-icon-size-large,
          var(--jh-size-900)
        );
      }
      :host([size='x-large']) {
        --icon-size: var(
          --jh-icon-size-extra-large,
          var(--jh-size-1400)
        );
      }
      svg {
        width: 100%;
        height: 100%;
      }
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="graphics-symbol",this.#e.ariaHidden="true",this.size="medium"}render(){return U`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="m4.672 4.673.708-.707 2.956 2.956c1.06-.42 2.261-.671 3.664-.671 4.354 0 6.76 2.427 9.066 4.753l.464.466a.75.75 0 0 1 0 1.061l-.464.467c-1.094 1.103-2.21 2.23-3.546 3.108l2.515 2.515-.707.708L4.672 4.673ZM6.48 7.895c-1.336.88-2.452 2.005-3.546 3.109l-.464.466a.75.75 0 0 0 0 1.061l.464.467C5.24 15.324 7.646 17.75 12 17.75c1.403 0 2.604-.252 3.664-.672l-1.176-1.176A8.57 8.57 0 0 1 12 16.25c-3.65 0-5.635-1.933-7.941-4.25 1.166-1.172 2.25-2.245 3.507-3.02L6.48 7.895Zm2.812 2.812a3 3 0 0 0 4 4.001l-4-4Zm.22-2.609 1.195 1.195a3 3 0 0 1 4 4l1.727 1.727c1.258-.775 2.341-1.848 3.507-3.02-2.306-2.316-4.29-4.25-7.941-4.25a8.57 8.57 0 0 0-2.488.348Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-eye-slash",gt);var wa=0,Lo=class extends g{static get formAssociated(){return!0}#e;#t;#o;static get styles(){return y`
      :host {
        font: var(--jh-font-helper-regular);
        display: inline-block;
        width: 100%;
        --icon-wrapper: var(--jh-size-800);
        --padding-no-slotted-content: var(--jh-space-400);
        --padding-with-slotted-content: calc(
          var(--jh-space-200) + var(--jh-space-400) + var(--jh-size-800)
        );
        --padding-double-slotted-content: calc(
          var(--jh-space-200) + var(--jh-space-200) + var(--jh-space-400) +
            var(--jh-size-800) + var(--jh-size-800)
        );
        --textarea-small-padding: calc(var(--jh-size-200) + var(--jh-size-50));
        --textarea-medium-padding: calc(var(--jh-size-300) + var(--jh-size-50));
        --textarea-large-padding: calc(var(--jh-size-400) + var(--jh-size-50));
        --set-icon-size: var(--jh-size-600);
      }
      label {
        color: var(
          --jh-input-label-color-text,
          var(--jh-color-content-primary-enabled)
        );
      }
      .helper-text {
        color: var(
          --jh-input-helper-color-text,
          var(--jh-color-content-secondary-enabled)
        );
        margin: 0;
      }
      label,
      .helper-text,
      .error-text {
        word-break: normal;
        overflow-wrap: anywhere;
      }
      .input-container {
        position: relative;
      }
      textarea {
        display: block;
      }
      :host([label]) .input-container,
      :host([label]) textarea {
        margin-top: var(--jh-space-200);
      }
      input,
      textarea,
      :host([hide-left-slot][hide-right-slot]) input {
        background-color: var(
          --jh-input-color-background,
          var(--jh-color-container-primary-enabled)
        );
        border: 1px solid
          var(--jh-input-color-border-enabled, var(--jh-color-divider-primary));
        border-radius: var(
          --jh-input-border-radius,
          var(--jh-border-radius-100)
        );
        color: var(
          --jh-input-color-text,
          var(--jh-color-content-primary-enabled)
        );
        font: var(--jh-font-body-regular-1);
        box-sizing: border-box;
      }
      input {
        width: 100%;
      }
      /* Sizes */
      :host([size='small']) input {
        height: var(--jh-size-1000);
      }
      :host([size='small']) textarea {
        min-height: var(
          --jh-input-size-min-height-text-area-small,
          var(--jh-size-2000)
        );
        padding: var(--textarea-small-padding, var(--jh-space-400));
        scroll-padding: var(--textarea-small-padding, var(--jh-space-400));
      }
      :host([size='medium']) input {
        height: var(--jh-size-1200);
      }
      :host([size='medium']) textarea {
        min-height: var(
          --jh-input-size-min-height-text-area-medium,
          var(--jh-size-2200)
        );
        padding: var(--textarea-medium-padding, var(--jh-space-400));
        scroll-padding: var(--textarea-medium-padding, var(--jh-space-400));
      }
      :host([size='large']) input {
        height: var(--jh-size-1400);
      }
      :host([size='large']) textarea {
        min-height: var(
          --jh-input-size-min-height-text-area-large,
          var(--jh-size-2400)
        );
        padding: var(--textarea-large-padding, var(--jh-space-400));
        scroll-padding: var(--textarea-large-padding, var(--jh-space-400));
      }
      /* Input padding */
      input,
      :host([hide-left-slot]) input,
      :host([type='search'][hide-left-slot]) input,
      .input-right-slot {
        padding-left: var(--padding-no-slotted-content);
      }
      input,
      :host([type='password'][hide-right-slot]) input:placeholder-shown,
      :host([type="password"][hide-right-slot]) input {
        padding-right: var(--padding-no-slotted-content);
      }
      .input-right-slot,
      :host([type='password']) input,
      :host([type='password']) input:placeholder-shown {
        padding-right: var(--padding-with-slotted-content);
      }
      :host([type='search']) input,
      .input-left-slot {
        padding-left: var(--padding-with-slotted-content);
      }
      .input-both-slots {
        padding: 0 var(--padding-with-slotted-content);
      }
      input::placeholder,
      textarea::placeholder {
        color: var(
          --jh-input-placeholder-color-text,
          var(--jh-color-content-secondary-enabled)
        );
      }
      .error-text {
        color: var(
          --jh-input-error-color-text,
          var(--jh-color-content-negative-enabled)
        );
        margin: var(--jh-space-200) 0 0 0;
      }
      /* Textarea */
      :host(:not([cols])) textarea {
        min-width: 100%;
      }
      :host([rows]) textarea {
        min-height: 0;
      }
      /* Textarea Resize/autoGrow */
      :host([auto-grow]) textarea,
      :host([no-resize]) textarea {
        resize: none;
        height: 100%;
      }
      :host([auto-grow]) textarea::-webkit-scrollbar {
        display: none;
      }
      :host([auto-grow]) textarea {
        overflow: hidden;
        -ms-overflow-style: none;
        scrollbar-width: none;
      }
      .left-slot,
      .right-slot {
        --jh-icon-size-extra-small: var(--set-icon-size);
        --jh-icon-size-small: var(--set-icon-size);
        --jh-icon-size-medium: var(--set-icon-size);
        --jh-icon-size-large: var(--set-icon-size);
        --jh-icon-size-extra-large: var(--set-icon-size);
      }
      /* icon positioning */
      .left-slot,
      .right-slot,
      .clear-button {
        height: var(--icon-wrapper);
        width: var(--icon-wrapper);
        position: absolute;
      }
      :host([size='small']) .left-slot,
      :host([size='small']) .right-slot,
      :host([size='small']) .clear-button {
        top: 4px;
      }
      :host([size='medium']) .left-slot,
      :host([size='medium']) .right-slot,
      :host([size='medium']) .clear-button {
        top: 8px;
      }
      :host([size='large']) .left-slot,
      :host([size='large']) .right-slot,
      :host([size='large']) .clear-button {
        top: 12px;
      }
      .left-slot {
        --jh-icon-color-fill: var(
          --jh-input-left-icon-color-fill,
          var(--jh-color-content-secondary-enabled)
        );
        --jh-button-size: var(--jh-size-800);
        left: var(--jh-space-400);
      }
      .right-slot {
        --jh-icon-color-fill: var(
          --jh-input-right-icon-color-fill,
          var(--jh-color-content-secondary-enabled)
        );
        --jh-button-size: var(--jh-size-800);
        right: var(--jh-space-400);
      }
      /* remove unused left and right icon styles */
      .left-slot,
      .right-slot {
        display: none;
      }
      .clear-button,
      :host([type='search']) .left-slot,
      :host([type='password']) .right-slot,
      .input-left-slot ~ .left-slot,
      .input-both-slots ~ .left-slot,
      .input-both-slots ~ .right-slot,
      .input-right-slot ~ .right-slot {
        display: flex;
        align-items: center;
        justify-content: center;
      }
      .clear-button,
      :host([type='password'][hide-right-slot]) .clear-button,
      :host([hide-right-slot]) .clear-button {
        right: var(--jh-space-400);
      }
      :host([type='password']) .clear-button,
      .input-right-slot ~ .clear-button,
      .input-both-slots ~ .clear-button {
        right: calc(
          var(--jh-space-200) + var(--jh-space-400) + var(--jh-size-800)
        );
      }
      /* Clear button icon styles */
      .clear-button {
        --jh-button-size: var(--jh-size-800);
        --jh-button-border-radius: var(--jh-input-clear-border-radius);
        --jh-button-color-background-tertiary-enabled: var(
          --jh-input-clear-color-background-enabled
        );
        --jh-button-color-border-tertiary-enabled: var(
          --jh-input-clear-color-border-enabled
        );
        --jh-button-icon-color-fill-tertiary-enabled: var(
          --jh-input-clear-icon-color-fill-enabled
        );
        --jh-button-color-background-tertiary-focus: var(
          --jh-input-clear-color-background-focus
        );
        --jh-button-color-border-tertiary-focus: var(
          --jh-input-clear-color-border-focus
        );
        --jh-button-shadow-focus: var(--jh-input-clear-shadow-focus);
        --jh-button-icon-color-fill-tertiary-focus: var(
          --jh-input-clear-icon-color-fill-focus
        );
        --jh-button-color-background-tertiary-hover: var(
          --jh-input-clear-color-background-hover
        );
        --jh-button-color-border-tertiary-hover: var(
          --jh-input-clear-color-border-hover
        );
        --jh-button-icon-color-fill-tertiary-hover: var(
          --jh-input-clear-icon-color-fill-hover
        );
        --jh-button-color-background-tertiary-active: var(
          --jh-input-clear-color-background-active
        );
        --jh-button-color-border-tertiary-active: var(
          --jh-input-clear-color-border-active
        );
        --jh-button-icon-color-fill-tertiary-active: var(
          --jh-input-clear-icon-color-fill-active
        );
        --jh-button-color-background-tertiary-disabled: var(
          --jh-input-clear-color-background-disabled
        );
        --jh-button-color-border-tertiary-disabled: var(
          --jh-input-clear-color-border-disabled
        );
        --jh-button-opacity-disabled: var(--jh-input-clear-opacity-disabled);
        --jh-button-icon-color-fill-tertiary-disabled: var(
          --jh-input-clear-icon-color-fill-disabled
        );
      }
      .hide-clear-button {
        display: none;
      }
      /* show-clear-button input padding  */
      :host([show-clear-button][type='password'][hide-right-slot]) input:placeholder-shown,
      :host([show-clear-button]) input:placeholder-shown {
        padding-right: var(--padding-no-slotted-content);
      }
      :host([show-clear-button]) input,
      :host([show-clear-button][type='password'][hide-right-slot]) input,
      :host([show-clear-button][type='password']) input:placeholder-shown,
      :host([show-clear-button]) input:placeholder-shown.input-right-slot,
      :host([show-clear-button]) input:placeholder-shown.input-both-slots {
        padding-right: var(--padding-with-slotted-content);
      }
      :host([show-clear-button][type='password']) input,
      :host([show-clear-button]) .input-right-slot,
      :host([show-clear-button]) .input-both-slots {
        padding-right: var(--padding-double-slotted-content);
      }
      /* Input States */
      input:active,
      textarea:active {
        border-color: var(
          --jh-input-color-border-active,
          var(--jh-color-content-brand-active)
        );
      }
      :host([disabled]) {
        opacity: var(--jh-input-opacity-disabled, var(--jh-opacity-300));
      }
      :host([disabled]) input,
      :host([disabled]) textarea {
        border-color: var(
          --jh-input-color-border-disabled,
          var(--jh-color-divider-primary)
        );
      }
      input:focus-visible,
      textarea:focus-visible {
        box-shadow: var(--jh-input-shadow-focus, var(--jh-shadow-focus));
        border-color: var(
          --jh-input-color-border-focus,
          var(--jh-color-content-brand-hover)
        );
      }
      input:focus-visible,
      textarea:focus-visible {
        outline: none;
      }
      input:hover,
      textarea:hover {
        border-color: var(
          --jh-input-color-border-hover,
          var(--jh-color-content-brand-hover)
        );
      }
      :host([invalid]) input,
      :host([invalid]) textarea {
        border-color: var(
          --jh-input-color-border-error,
          var(--jh-color-content-negative-enabled)
        );
      }
      :host([invalid]) input:focus-visible,
      :host([invalid]) textarea:focus-visible {
        box-shadow: none;
        outline: none;
      }
      /* readonly styles */
      :host([readonly]) input,
      :host([readonly]) textarea,
      :host([readonly][type="search"][hide-left-slot]) input,
      :host([type="password"][readonly]) input, 
      :host([type="password"][readonly]) input:placeholder-shown,
      :host([show-clear-button][readonly]) input:placeholder-shown {
        height: auto;
        background-color: transparent;
        border: none;
        padding-left: 0;
        padding-right: 0;
      }
      :host([readonly]) textarea {
        padding-top: 0;
      }
      /* Optional/Required/Show-indicator */
      :host([show-indicator]) span {
        color: var(
          --jh-input-optional-color-text,
          var(--jh-color-content-primary-enabled)
        );
      }
      :host([show-indicator][required]) span {
        color: var(
          --jh-input-required-color-text,
          var(--jh-color-content-negative-enabled)
        );
      }
      /* remove native input browser styles */
      input::-webkit-search-decoration,
      input::-webkit-search-cancel-button,
      input::-webkit-search-results-button,
      input::-webkit-search-results-decoration {
        display: none;
      }
      input::-webkit-outer-spin-button,
      input::-webkit-inner-spin-button {
        -webkit-appearance: none;
      }
      ::-ms-reveal {
        display: none;
      }
      /* Remove chrome autocomplete input styles */
      input:-webkit-autofill {
        transition: background-color 5000s ease-in-out 0s;
      }
    `}static get properties(){return{accessibleLabelClearButton:{type:String,attribute:"accessible-label-clear-button"},accessibleLabelInput:{type:String,attribute:"accessible-label-input"},accessibleLabelHidePassword:{type:String,attribute:"accessible-label-hide-password"},accessibleLabelShowPassword:{type:String,attribute:"accessible-label-show-password"},autocomplete:{type:String},autoGrow:{type:Boolean,attribute:"auto-grow",reflect:!0},cols:{type:String},disabled:{type:Boolean,reflect:!0},enterkeyhint:{type:String},errorText:{type:String,attribute:"error-text"},helperText:{type:String,attribute:"helper-text",reflect:!0},hideLeftSlot:{type:Boolean,attribute:"hide-left-slot",reflect:!0},passwordVisible:{type:Boolean,attribute:"password-visible"},hideRightSlot:{type:Boolean,attribute:"hide-right-slot",reflect:!0},inputmode:{type:String},invalid:{type:Boolean,reflect:!0},label:{type:String},maxlength:{type:String},minlength:{type:String},name:{type:String},noResize:{type:Boolean,attribute:"no-resize",reflect:!0},placeholder:{type:String},readonly:{type:Boolean,reflect:!0},required:{type:Boolean,reflect:!0},rows:{type:String},showClearButton:{type:Boolean,attribute:"show-clear-button",reflect:!0},showIndicator:{type:Boolean,attribute:"show-indicator",reflect:!0},size:{type:String,reflect:!0},spellcheck:{type:String},type:{type:String,reflect:!0},value:{type:String},wrap:{type:String}}}constructor(){super(),this.#e=this.attachInternals(),this.accessibleLabelInput=null,this.accessibleLabelClearButton=null,this.accessibleLabelHidePassword=null,this.accessibleLabelShowPassword=null,this.autocomplete=null,this.autoGrow=!1,this.cols=null,this.disabled=!1,this.enterkeyhint=null,this.errorText=null,this.helperText=null,this.hideLeftSlot=!1,this.passwordVisible=!1,this.hideRightSlot=!1,this.inputmode=null,this.invalid=!1,this.label=null,this.maxlength=null,this.minlength=null,this.name=null,this.noResize=!1,this.placeholder=null,this.readonly=!1,this.required=!1,this.rows=null,this.showClearButton=!1,this.showIndicator=!1,this.size="medium",this.spellcheck=null,this.type="text",this.value=null,this.wrap=null}get form(){return this.#e.form}get validity(){return this.#e.validity}get value(){return this.#t}set value(e){let t=this.#t;e!==t&&(this.#t=e,this.#e.setFormValue(this.#t)),this.requestUpdate("value",t)}connectedCallback(){super.connectedCallback(),this.#o=wa++}#r(){this.value=null,this.shadowRoot.querySelector("input").focus(),this.#i("jh-clear")}#i(e){this.dispatchEvent(new CustomEvent(e,{bubbles:!0,cancelable:!0,composed:!0}))}#a(){!this.readonly&&!this.disabled&&(this.passwordVisible=!this.passwordVisible)}#s(){let e=!1,t=!1;for(let o of this.children)o.slot==="jh-input-left"?o.assignedSlot!==null&&(e=!0):(o.slot==="jh-input-right"||o.slot==="jh-input-password-visible"||o.slot==="jh-input-password-hidden")&&o.assignedSlot!==null&&(t=!0);this.#c(e,t)}#c(e,t){let o,i=this.shadowRoot.querySelector("input");e&&t?o="input-both-slots":e?o="input-left-slot":t&&(o="input-right-slot"),o?(i.className&&i.classList.remove(i.className),i.classList.add(o)):i.classList.remove(i.className)}#h(){let e=this.shadowRoot.querySelector("textarea");e.style.height="auto",e.style.height=e.scrollHeight+"px"}#n(){this.#i("jh-change")}#l(e){this.value=e.target.value,this.type==="textarea"&&this.autoGrow&&this.#h()}#d(e){let t=e.target.value.substring(e.target.selectionStart,e.target.selectionEnd);this.dispatchEvent(new CustomEvent("jh-select",{detail:{selected:t},bubbles:!0,cancelable:!0,composed:!0}))}#p(){return d`
      <slot
        class="left-slot"
        name="jh-input-left"
        @slotchange=${this.#s}
        >${this.type==="search"?d`
              <jh-icon-magnifying-glass aria-hidden="true" size="small"></jh-icon-magnifying-glass>
            `:null}
      </slot>
    `}#u(){let e=this.value?null:"hide-clear-button";return d`
      <jh-button size="small" appearance="tertiary" class="clear-button ${e}" 
      accessible-label=${v(this.accessibleLabelClearButton)} 
      ?disabled=${this.disabled||this.readonly} 
      @click=${this.#r}
      >
        <slot name="jh-input-clear-button" slot="jh-button-icon">
          <jh-icon-circle-xmark slot="jh-button-icon" aria-hidden="true" size="small"></jh-icon-circle-xmark>
        </slot>
      </jh-button>
    `}#b(){if(this.type==="password"){let e=this.passwordVisible?this.accessibleLabelHidePassword:this.accessibleLabelShowPassword;return d`
        <jh-button
          size="small"
          class="right-slot"
          appearance="tertiary"
          ?disabled=${this.disabled||this.readonly}
          accessible-label=${v(e)}
          @click=${this.#a}
        >
          ${this.passwordVisible?d`
                <slot
                  name="jh-input-password-visible"
                  slot="jh-button-icon"
                  @slotchange=${this.#s}
                >
                  <jh-icon-eye-slash
                    slot="jh-button-icon"
                    aria-hidden="true"
                    size="small"
                  ></jh-icon-eye-slash>
                </slot>
              `:d`
                <slot
                  name="jh-input-password-hidden"
                  slot="jh-button-icon"
                  @slotchange=${this.#s}
                >
                  <jh-icon-eye
                    slot="jh-button-icon"
                    aria-hidden="true"
                    size="small"
                  ></jh-icon-eye>
                </slot>
              `}
        </jh-button>
      `}else return d`
        <slot
          class="right-slot"
          name="jh-input-right"
          @slotchange=${this.#s}
        ></slot>
      `}#v(){if(this.readonly)return;let e=this.showClearButton&&!this.readonly&&!this.disabled,t=this.hideLeftSlot?null:this.#p(),o=e?this.#u():null,i;return this.hideRightSlot||this.type==="password"&&this.disabled?i=null:i=this.#b(),d`${t}${o}${i}`}#m(){if(this.label&&this.helperText&&this.errorText)return`jh-input-error-${this.#o} jh-input-desc-${this.#o}`;if(this.label&&this.helperText)return`jh-input-desc-${this.#o}`;if(this.invalid&&this.errorText)return`jh-input-error-${this.#o}`}render(){let e=["email","password","search","tel","text","textarea","url"],t,o,i,a,s,h,l=this.label&&this.helperText||this.invalid&&this.errorText?this.#m():null;return this.showIndicator&&(this.required?i=d`<span class="indicator" aria-hidden="true"> *</span>`:i=d`<span class="indicator"> (optional)</span>`),this.helperText&&(a=d`
        <p id="jh-input-desc-${this.#o}" class="helper-text">
          ${this.helperText}
        </p>
      `),this.label&&(t=d`
        <label for="jh-input-${this.#o}"> ${this.label}${i} </label>
        ${a}
      `),this.invalid&&this.errorText&&(s=d`
        <div id="jh-input-error-${this.#o}" class="error-text">
          ${this.errorText}
        </div>
      `),e.includes(this.type)&&(this.type==="password"&&this.passwordVisible?h="text":h=this.type),this.type==="textarea"?o=d`
        <textarea
          id="jh-input-${this.#o}"
          aria-describedby=${v(l)}
          aria-invalid=${v(this.invalid?"true":null)}
          aria-label=${v(this.accessibleLabelInput===""?null:this.accessibleLabelInput)}
          autocomplete=${v(this.autocomplete===""?null:this.autocomplete)}
          ?disabled=${this.disabled}
          inputmode=${v(this.inputmode===""?null:this.inputmode)}
          ?invalid=${this.invalid}
          maxlength=${v(this.maxlength===""?null:this.maxlength)}
          minlength=${v(this.minlength===""?null:this.minlength)}
          name=${v(this.name===""?null:this.name)}
          placeholder=${v(this.placeholder===""?null:this.placeholder)}
          ?readonly=${this.readonly}
          ?required=${this.required}
          spellcheck=${v(this.spellcheck===""?null:this.spellcheck)}
          .value=${this.value}
          @change=${this.#n}
          @input=${this.#l}
          @select=${this.#d}
          cols=${v(this.cols===""?null:this.cols)}
          rows=${v(this.rows===""?null:this.rows)}
          wrap=${v(this.wrap===""?null:this.wrap)}
        ></textarea>
      `:o=d`
        <div class="input-container">
          <input
            id="jh-input-${this.#o}"
            aria-describedby=${v(l)}
            aria-invalid=${v(this.invalid?"true":null)}
            aria-label=${v(this.accessibleLabelInput===""?null:this.accessibleLabelInput)}
            autocomplete=${v(this.autocomplete===""?null:this.autocomplete)}
            ?disabled=${this.disabled}
            inputmode=${v(this.inputmode===""?null:this.inputmode)}
            ?invalid=${this.invalid}
            maxlength=${v(this.maxlength===""?null:this.maxlength)}
            minlength=${v(this.minlength===""?null:this.minlength)}
            name=${v(this.name===""?null:this.name)}
            placeholder=${v(this.placeholder===""?null:this.placeholder)}
            ?readonly=${this.readonly}
            ?required=${this.required}
            spellcheck=${v(this.spellcheck===""?null:this.spellcheck)}
            .value=${this.value}
            @change=${this.#n}
            @input=${this.#l}
            @select=${this.#d}
            type=${v(h)}
          />${this.#v()}
        </div>
      `,d` ${t} ${o} ${s} `}};customElements.define("jh-input",Lo);var ka=0,Ro=class extends g{static get formAssociated(){return!0}#e;#t;#o;#r;static get styles(){return y`
      :host {
        font: var(--jh-font-helper-regular);
        display: block;
      }
      /* reset styling on fieldset and legend */
      :host fieldset {
        border: none;
        padding: 0;
        margin: 0;
      }
      :host legend {
        padding: 0;
      }
      :host([label]) .controls {
        margin-top: var(--jh-space-200);
      }
      :host([orientation='vertical']) .controls {
        display: flex;
        flex-direction: column;
      }
      :host([orientation='vertical']) ::slotted(*) {
        margin-bottom: var(--jh-space-200);
        flex: 1;
      }
      :host([orientation='vertical']) ::slotted(:last-of-type) {
        margin-bottom: 0;
      }
      :host([orientation='horizontal']) .controls {
        display: flex;
        flex-direction: row;
      }
      :host([orientation='horizontal']) ::slotted(*) {
        margin-right: var(--jh-space-400);
        margin-bottom: 0;
        flex: 1;
      }
      :host([orientation='horizontal']) ::slotted(:last-of-type) {
        margin-right: 0;
      }
      .label {
        color: var(
          --jh-radio-group-label-color-text,
          var(--jh-color-content-primary-enabled)
        );
      }
      .helper-text {
        color: var(
          --jh-radio-group-helper-color-text,
          var(--jh-color-content-secondary-enabled)
        );
        margin: 0;
      }
      .label-text,
      .helper-text,
      :host([invalid]) .error-text {
        word-break: break-word;
      }
      :host([invalid]) .error-text {
        color: var(
          --jh-radio-group-error-color-text,
          var(--jh-color-content-negative-enabled)
        );
        margin: var(--jh-space-200) 0 0 0;
      }
      :host([show-indicator]) .indicator {
        color: var(
          --jh-radio-group-required-color-text-optional,
          var(--jh-color-content-primary-enabled)
        );
        font: var(--jh-font-helper-regular);
      }
      :host([show-indicator][required]) .indicator {
        color: var(
          --jh-radio-group-required-color-text,
          var(--jh-color-content-negative-enabled)
        );
      }
    `}static get properties(){return{accessibleLabel:{type:String,attribute:"accessible-label"},errorText:{type:String,attribute:"error-text"},helperText:{attribute:"helper-text"},invalid:{type:Boolean,reflect:!0},label:{type:String},name:{type:String},required:{type:Boolean,reflect:!0},orientation:{type:String,reflect:!0},showIndicator:{type:Boolean,reflect:!0,attribute:"show-indicator"},value:{type:String,reflect:!0}}}constructor(){super(),this.#o=this.attachInternals(),this.accessibleLabel=null,this.errorText=null,this.helperText=null,this.invalid=!1,this.label=null,this.name=null,this.required=!1,this.orientation="vertical",this.showIndicator=!1,this.value=null,this.addEventListener("jh-change",this.#s),this.addEventListener("keydown",this.#c),this.addEventListener("focusout",this.#h)}connectedCallback(){super.connectedCallback(),this.#t=ka++}get form(){return this.#o.form}get validity(){return this.#o.validity}get value(){return this.#r}set value(e){let t=this.#r;e!==t&&(this.#r=e,this.#o.setFormValue(e)),this.requestUpdate("value",t)}#i(){return[...this.querySelectorAll("jh-radio")]}#a(){let e=this.#i();if(this.#e=e.find(t=>t.value===this.value&&this.value!==null),!this.#e){let t=e.find(o=>o.checked);t?(this.value=t.value,this.#e=t):(this.value=null,this.#e=null)}this.#n(),this.#e||(e[0].tabIndex=0)}#s(e){if(e.target.tagName==="JH-RADIO"){this.value=e.target.value,this.#e=e.target,this.#n();let t={bubbles:!0,composed:!0,cancelable:!0};this.dispatchEvent(new CustomEvent("jh-change",t))}}#c(e){if(!["ArrowDown","ArrowRight","ArrowUp","ArrowLeft","Space"].includes(e.code))return;let o=this.#i().filter(s=>!s.disabled),i=o.length,a=o.findIndex(s=>s.checked);a=a===-1?0:a,e.code==="ArrowDown"||e.code==="ArrowRight"?(e.preventDefault(),a+1<i?a++:a=0):(e.code==="ArrowUp"||e.code==="ArrowLeft")&&(e.preventDefault(),a-1>=0?a--:a=i-1),this.value=o[a].value,o[a].click(),this.#e=o[a],this.#n(),o[a].focus()}#h(){let e=this.#i().filter(t=>!t.disabled);e.some(t=>t.checked)&&e.forEach(t=>t.checked?t.tabIndex=0:t.tabIndex=-1)}#n(){this.#i().forEach(t=>{t.checked=this.#e===t,t.checked?t.tabIndex=0:t.tabIndex=-1})}#l(){if(this.errorText&&this.invalid&&this.helperText&&this.label)return`radio-group-error-${this.#t} radio-group-helper-${this.#t}`;if(this.errorText&&this.invalid)return`radio-group-error-${this.#t}`;if(this.helperText&&this.label)return`radio-group-helper-${this.#t}`}render(){let e,t,o,i;return this.showIndicator&&(this.required?e=d`<span class="indicator" aria-hidden="true"> *</span>`:e=d`<span class="indicator"> (optional)</span>`),this.helperText&&(t=d`<p
        class="helper-text"
        id="radio-group-helper-${this.#t}"
      >
        ${this.helperText}
      </p>`),this.label&&(o=d`<legend class="label" for="radio-group-label-${this.#t}">
          ${this.label}${e}
        </legend>
        ${t}`),this.invalid&&this.errorText&&(i=d`<p class="error-text" id="radio-group-error-${this.#t}">
        ${this.errorText}
      </p>`),d`
      <fieldset
        role="radiogroup"
        id=${v(this.label?`radio-group-label-${this.#t}`:null)}
        aria-describedby=${v(this.#l())}
        aria-required=${v(this.required?"true":"false")}
        aria-invalid=${v(this.invalid?"true":null)}
        aria-label=${v(this.accessibleLabel)}
      >
        ${o}
        <div class="controls">
          <slot @slotchange=${this.#a}></slot>
        </div>
        ${i}
      </fieldset>
    `}};customElements.define("jh-radio-group",Ro);var Aa=0,Po=class extends g{#e;#t;static get styles(){return y`
      :host {
        font: var(--jh-font-body-regular-1);
        display: inline-flex;
        position: relative;
      }
      span {
        box-sizing: border-box;
        display: block;
      }
      .radio {
        border: 1px solid
          var(
            --jh-radio-input-color-border-unselected-enabled,
            var(--jh-color-divider-primary)
          );
        background-color: var(
          --jh-radio-input-color-background-unselected-enabled,
          var(--jh-color-container-primary-enabled)
        );
        border-radius: var(
          --jh-radio-input-border-radius,
          var(--jh-border-radius-circle)
        );
        width: var(--jh-size-500);
        height: var(--jh-size-500);
        position: relative;
      }
      :host(:hover) {
        cursor: pointer;
      }
      :host(:focus) {
        outline: none;
      }
      :host(:focus-visible) .radio {
        box-shadow: var(--jh-radio-input-shadow-focus, var(--jh-shadow-focus));
        background-color: var(
          --jh-radio-input-color-background-unselected-focus,
          var(--jh-color-container-primary-enabled)
        );
        border-color: var(
          --jh-radio-input-color-border-unselected-focus,
          var(--jh-color-content-brand-hover)
        );
      }
      :host .radio:hover {
        background-color: var(
          --jh-radio-input-color-background-unselected-hover,
          var(--jh-color-container-primary-enabled)
        );
        border-color: var(
          --jh-radio-input-color-border-unselected-hover,
          var(--jh-color-content-brand-hover)
        );
      }
      :host .radio:active {
        background-color: var(
          --jh-radio-input-color-background-unselected-active,
          var(--jh-color-container-primary-enabled)
        );
        border-color: var(
          --jh-radio-input-color-border-unselected-active,
          var(--jh-color-content-brand-active)
        );
      }
      :host([disabled]) .radio {
        background-color: var(
          --jh-radio-input-color-background-unselected-disabled,
          var(--jh-color-container-primary-enabled)
        );
        border-color: var(
          --jh-radio-input-color-border-unselected-disabled,
          var(--jh-color-divider-primary)
        );
      }
      :host([checked]) .radio {
        background-color: var(
          --jh-radio-input-color-background-selected-enabled,
          var(--jh-color-container-primary-enabled)
        );
        border-color: var(
          --jh-radio-input-color-border-selected-enabled,
          var(--jh-color-content-brand-enabled)
        );
      }
      :host([checked]:focus-visible) .radio {
        box-shadow: var(--jh-radio-input-shadow-focus, var(--jh-shadow-focus));
        background-color: var(
          --jh-radio-input-color-background-selected-focus,
          var(--jh-color-container-primary-enabled)
        );
        border-color: var(
          --jh-radio-input-color-border-selected-focus,
          var(--jh-color-content-brand-hover)
        );
      }
      :host([checked]) .radio:hover {
        background-color: var(
          --jh-radio-input-color-background-selected-hover,
          var(--jh-color-container-primary-enabled)
        );
        border-color: var(
          --jh-radio-input-color-border-selected-hover,
          var(--jh-color-content-brand-hover)
        );
      }
      :host([checked]) .radio:active {
        background-color: var(
          --jh-radio-input-color-background-selected-active,
          var(--jh-color-container-primary-enabled)
        );
        border-color: var(
          --jh-radio-input-color-border-selected-active,
          var(--jh-color-content-brand-active)
        );
      }
      :host([checked][disabled]) .radio {
        background-color: var(
          --jh-radio-input-color-background-selected-disabled,
          var(--jh-color-container-primary-enabled)
        );
        border-color: var(
          --jh-radio-input-color-border-selected-disabled,
          var(--jh-color-content-brand-enabled)
        );
      }
      :host([checked]) .radio::before {
        background-color: var(
          --jh-radio-status-color-background-selected-enabled,
          var(--jh-color-content-brand-enabled)
        );
        border-radius: var(
          --jh-radio-input-border-radius,
          var(--jh-border-radius-circle)
        );
        width: var(--jh-size-300);
        height: var(--jh-size-300);
        content: '';
        position: absolute;
        top: 3px;
        left: 3px;
      }
      :host([checked]:focus-visible) .radio::before {
        background-color: var(
          --jh-radio-status-color-background-selected-focus,
          var(--jh-color-content-brand-hover)
        );
      }
      :host([checked]) .radio:hover::before {
        background-color: var(
          --jh-radio-status-color-background-selected-hover,
          var(--jh-color-content-brand-hover)
        );
      }
      :host([checked]) .radio:active::before {
        background-color: var(
          --jh-radio-status-color-background-selected-active,
          var(--jh-color-content-brand-active)
        );
      }
      :host([checked][disabled]) .radio::before {
        background-color: var(
          --jh-radio-status-color-background-selected-disabled,
          var(--jh-color-content-brand-enabled)
        );
      }
      .label-container {
        margin-left: var(--jh-space-200);
        flex: 1;
      }
      .label-text,
      .helper-text {
        word-break: break-word;
      }
      .label-text {
        color: var(
          --jh-radio-label-color-text,
          var(--jh-color-content-primary-enabled)
        );
      }
      .helper-text {
        color: var(
          --jh-radio-helper-color-text,
          var(--jh-color-content-secondary-enabled)
        );
        font: var(--jh-font-helper-regular);
        margin: 0;
      }
      :host([disabled]) {
        opacity: var(--jh-radio-opacity-disabled, var(--jh-opacity-disabled));
        cursor: default;
        box-shadow: none;
      }
      :host([disabled]:focus-visible) .radio {
        box-shadow: none;
      }
    `}static get properties(){return{accessibleLabel:{type:String,attribute:"accessible-label"},checked:{type:Boolean,reflect:!0},disabled:{type:Boolean,reflect:!0},helperText:{type:String,attribute:"helper-text"},label:{type:String},value:{type:String}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="radio",this.accessibleLabel=null,this.checked=!1,this.disabled=!1,this.helperText=null,this.label=null,this.value=null,this.addEventListener("click",this.#r),this.addEventListener("keydown",this.#i)}connectedCallback(){super.connectedCallback(),this.#e=Aa++,this.setAttribute("tabindex","0"),this.setAttribute("aria-checked",`${this.checked}`),this.accessibleLabel&&this.setAttribute("aria-label",`${this.accessibleLabel}`),this.helperText&&this.setAttribute("aria-describedby",`radio-helper-text-${this.#e}`);let e=new MutationObserver(this.#o.bind(this)),t={attributes:!0};e.observe(this,t)}#o(e){for(let t of e)t.attributeName==="checked"&&this.setAttribute("aria-checked",`${this.checked}`),t.attributeName==="disabled"&&this.setAttribute("aria-disabled",`${this.disabled}`),t.attributeName==="accessible-label"&&this.setAttribute("aria-label",`${this.accessibleLabel}`)}#r(){if(this.disabled||this.checked)return;this.checked=!0;let e={bubbles:!0,composed:!0,cancelable:!0};this.dispatchEvent(new CustomEvent("jh-change",e))}#i(e){e.code==="Space"&&this.#r()}render(){let e,t;return this.helperText&&(e=d`
        <p class="helper-text" id="radio-helper-text-${this.#e}">
          ${this.helperText}
        </p>
      `),this.label&&(t=d`
        <span class="label-container">
          <span class="label-text">${this.label}</span>
          ${e}
        </span>
      `),d`
      <span class="radio" aria-hidden="true"></span>
      ${t}
    `}};customElements.define("jh-radio",Po);var Oo=class extends g{static get styles(){return y`
    :host {
      font: var(--jh-font-body-regular-1);
      padding: var(--jh-space-300) var(--jh-space-400);
      display: block;
    }
    :host([type='alert']) {
      border-radius: var(
        --jh-notification-border-radius-alert,
        var(--jh-border-radius-100)
      );
    }
    :host([type='banner']) {
      border-radius: var(
        --jh-notification-border-radius-banner,
        var(--jh-border-radius-0)
      );
      position: absolute;
      top: 0;
      left: 0;
      width: 100%;
      box-sizing: border-box;
    }
    /* Appearances */
    :host([appearance='neutral']) {
      background-color: var(
        --jh-notification-color-background-neutral,
        var(--jh-color-content-primary-enabled)
      );
      color: var(
        --jh-notification-color-text-neutral,
        var(--jh-color-content-on-primary-enabled)
      );
    }
    :host([appearance='positive']) {
      background-color: var(
        --jh-notification-color-background-positive,
        var(--jh-color-content-positive-enabled)
      );
      color: var(
        --jh-notification-color-text-positive,
        var(--jh-color-content-on-positive-enabled)
      );
    }
    :host([appearance='negative']) {
      background-color: var(
        --jh-notification-color-background-negative,
        var(--jh-color-content-negative-enabled)
      );
      color: var(
        --jh-notification-color-text-negative,
        var(--jh-color-content-on-negative-enabled)
      );
    }
    :host([appearance='neutral']) jh-button {
      --jh-button-shadow-focus: var(--jh-notification-dismiss-shadow-neutral-focus, 0px 0px 0px 1px var(--jh-color-content-primary-enabled), 0px 0px 0px 3px var(--jh-color-content-inverse-hover));
    }
    :host([appearance='positive']) jh-button {
      --jh-button-shadow-focus: var(--jh-notification-dismiss-shadow-positive-focus, 0px 0px 0px 1px var(--jh-color-content-positive-enabled), 0px 0px 0px 3px var(--jh-color-content-inverse-hover));
    }
    :host([appearance='negative']) jh-button {
      --jh-button-shadow-focus: var(--jh-notification-dismiss-shadow-negative-focus, 0px 0px 0px 1px var(--jh-color-content-negative-enabled), 0px 0px 0px 3px var(--jh-color-content-inverse-hover));
    }
    jh-button {
      --jh-button-icon-color-fill-secondary-enabled: var(--jh-notification-dismiss-icon-color-enabled, var(--jh-color-content-inverse-enabled));
      --jh-button-color-background-secondary-enabled: var(--jh-notification-dismiss-color-background-enabled, (transparent));
      --jh-button-color-border-secondary-enabled: var(--jh-notification-dismiss-color-border-enabled, (transparent));
      --jh-button-icon-color-fill-secondary-active: var(--jh-notification-dismiss-icon-color-active, var(--jh-color-content-on-inverse-active));
      --jh-button-color-background-secondary-active: var(--jh-notification-dismiss-color-background-active, var(--jh-color-content-inverse-active));
      --jh-button-color-border-secondary-active: var(--jh-notification-dismiss-color-border-active, (transparent));
      --jh-button-icon-color-fill-secondary-focus: var(--jh-notification-dismiss-icon-color-focus, var(--jh-color-content-on-inverse-hover));
      --jh-button-color-background-secondary-focus: var(--jh-notification-dismiss-color-background-focus, var(--jh-color-content-inverse-hover));
      --jh-button-color-border-secondary-focus: var(--jh-notification-dismiss-color-border-focus, (transparent));
      --jh-button-icon-color-fill-secondary-hover: var(--jh-notification-dismiss-icon-color-hover, var(--jh-color-content-on-inverse-hover));
      --jh-button-color-background-secondary-hover: var(--jh-notification-dismiss-color-background-hover, var(--jh-color-content-inverse-hover));
      --jh-button-color-border-secondary-hover: var(--jh-notification-dismiss-color-border-hover, (transparent));
      margin-left: var(--jh-space-400);
    }
    /* Default slot styling */
    .display-default-slot {
      padding: var(--jh-space-200) 0;
      margin: var(--jh-space-50) 0;
      font: var(--jh-font-body-regular-1);
      display: flex;
      flex: 1;
      word-break: break-all;
    }
    /* Icon slot styling */
    slot[name="jh-notification-icon"] {
      display: block;
    }
    slot[name="jh-notification-icon"]::slotted(*) {
      margin-right: var(--jh-space-400);
      padding: var(--jh-space-200) 0;
    }
    :host([appearance='neutral']) slot[name="jh-notification-icon"] {
      --jh-icon-color-fill: var(--jh-notification-icon-color-fill-neutral, var(--jh-color-content-on-primary-enabled));
    }
    :host([appearance='positive']) slot[name="jh-notification-icon"] {
      --jh-icon-color-fill: var(--jh-notification-icon-color-fill-positive, var(--jh-color-content-on-positive-enabled));
    }
    :host([appearance='negative']) slot[name="jh-notification-icon"] {
      --jh-icon-color-fill: var(--jh-notification-icon-color-fill-negative, var(--jh-color-content-on-negative-enabled));
    }
    /* action slot styling*/
    :host([appearance='neutral']) slot[name="jh-notification-action"]::slotted(*) {
      --jh-button-shadow-focus: var(--jh-notification-action-shadow-neutral-focus, 0px 0px 0px 1px var(--jh-color-content-primary-enabled), 0px 0px 0px 3px var(--jh-color-content-inverse-hover));
    }
    :host([appearance='positive']) slot[name="jh-notification-action"]::slotted(*) {
      --jh-button-shadow-focus: var(--jh-notification-action-shadow-positive-focus, 0px 0px 0px 1px var(--jh-color-content-positive-enabled), 0px 0px 0px 3px var(--jh-color-content-inverse-hover));
    }
    :host([appearance='negative']) slot[name="jh-notification-action"]::slotted(*) {
      --jh-button-shadow-focus: var(--jh-notification-action-shadow-negative-focus, 0px 0px 0px 1px var(--jh-color-content-negative-enabled), 0px 0px 0px 3px var(--jh-color-content-inverse-hover));
    }
    slot[name="jh-notification-action"]::slotted(*) {
      --jh-button-label-color-text-secondary-enabled: var(--jh-notification-action-label-color-text-enabled, var(--jh-color-content-inverse-enabled));
      --jh-button-color-background-secondary-enabled: var(--jh-notification-action-color-background-enabled, (transparent));
      --jh-button-color-border-secondary-enabled: var(--jh-notification-action-color-border-enabled, (transparent));
      --jh-button-label-color-text-secondary-focus: var(--jh-notification-action-label-color-text-focus, var(--jh-color-content-on-inverse-hover));
      --jh-button-color-background-secondary-focus: var(--jh-notification-action-color-background-focus, var(--jh-color-content-inverse-hover));
      --jh-button-color-border-secondary-focus: var(--jh-notification-action-color-border-focus, (transparent));
      --jh-button-label-color-text-secondary-hover: var(--jh-notification-action-label-color-text-hover, var(--jh-color-content-on-inverse-hover));
      --jh-button-color-background-secondary-hover: var(--jh-notification-action-color-background-hover, var(--jh-color-content-inverse-hover));
      --jh-button-color-border-secondary-hover: var(--jh-notification-action-color-border-hover, (transparent));
      --jh-button-label-color-text-secondary-active: var(--jh-notification-action-label-color-text-active, var(--jh-color-content-on-inverse-active));
      --jh-button-color-background-secondary-active: var(--jh-notification-action-color-background-active, var(--jh-color-content-inverse-active));
      --jh-button-color-border-secondary-active: var(--jh-notification-action-color-border-active, (transparent));
      --jh-button-color-background-secondary-pending: var(--jh-notification-action-color-background-pending, (transparent));
      --jh-button-color-border-secondary-pending: var(--jh-notification-action-color-border-pending, (transparent));
      --jh-button-icon-color-fill-secondary-enabled: var(--jh-notification-action-icon-color-fill-enabled, var(--jh-color-content-inverse-enabled));
      --jh-button-icon-color-fill-secondary-focus: var(--jh-notification-action-icon-color-fill-focus, var(--jh-color-content-on-inverse-hover));
      --jh-button-icon-color-fill-secondary-hover: var(--jh-notification-action-icon-color-fill-hover, var(--jh-color-content-on-inverse-hover));
      --jh-button-icon-color-fill-secondary-active: var(--jh-notification-action-icon-color-fill-active, var(--jh-color-content-on-inverse-active));
      --jh-button-progress-color-border-secondary-pending: var(--jh-notification-action-progress-color-border-pending, var(--jh-color-content-inverse-enabled));
    }
    slot[name="jh-notification-action"]::slotted(:first-of-type) {
      margin-left: var(--jh-space-400);
    }
    slot[name="jh-notification-action"]::slotted(:not(:first-of-type)) {
      margin-left: var(--jh-space-200);
    }
    :host([stacked]) {
      display: flex;
      flex-direction: column;
    }
    :host([stacked]) slot[name="jh-notification-action"]::slotted(:first-of-type) {
      margin-left: 0;
    }
    .stacked-action-btn {
      margin-top: var(--jh-space-400);
      display: flex;
      justify-content: flex-end;
    }
    .stacked-container,
    .inline-container {
      display: flex;
      align-items: flex-start;
    }
    `}static get properties(){return{appearance:{type:String,reflect:!0},dismissButtonAccessibleLabel:{type:String,attribute:"dismiss-button-accessible-label"},hideDismissButton:{type:Boolean,attribute:"hide-dismiss-button"},stacked:{type:Boolean,reflect:!0},type:{type:String,reflect:!0}}}constructor(){super(),this.appearance="neutral",this.dismissButtonAccessibleLabel=null,this.hideDismissButton=!1,this.stacked=!1,this.type="alert"}#e(){this.#t("jh-dismiss"),this.parentNode.host?.nodeName!=="JH-TOAST"&&this.remove()}#t(e){this.dispatchEvent(new CustomEvent(e,{bubbles:!0,cancelable:!0,composed:!0}))}#o(){let e=this.stacked?"stacked-action-btn":"inline-action-btn";return d`
        <slot name="jh-notification-action" class=${e} @slotchange=${this.#r}></slot>
    `}#r(){let e=this.querySelectorAll("jh-button");for(let t of e)t.getAttribute("size")!=="small"&&t.setAttribute("size","small")}#i(e){e&&this.shadowRoot.querySelector("slot:not([name])").classList.add("display-default-slot")}render(){let e;return this.hideDismissButton||(e=d`
        <jh-button
          size="small"
          appearance="secondary"
          accessible-label=${this.dismissButtonAccessibleLabel}
          @click=${this.#e}
          ><slot name="jh-notification-dismiss-icon" slot="jh-button-icon">
            <jh-icon-xmark slot="jh-button-icon"></jh-icon-xmark>
          </slot>
        </jh-button>
      `),d`
      <div class=${this.stacked?"stacked-container":"inline-container"}>
        <slot aria-hidden="true" name="jh-notification-icon"></slot>
        <slot @slotchange=${this.#i}></slot>
        ${this.stacked?null:this.#o()}
        ${e}
      </div>
      ${this.stacked?this.#o():null}
    `}};customElements.define("jh-notification",Oo);var Sa=0,No=class extends g{#e;static get styles(){return y`
      :host {
        display: block;
        box-sizing: border-box;
      }
      .subheader {
        background-color: var(
          --jh-list-group-subheader-color-background,
          transparent
        );
        color: var(
          --jh-list-group-subheader-color-text,
          var(--jh-color-content-secondary-enabled)
        );
        font: var(--jh-font-heading-medium-1);
        padding-top: var(--jh-space-400);
        padding-right: var(
          --jh-list-group-subheader-space-padding-right,
          var(--jh-space-600)
        );
        padding-bottom: var(--jh-space-400);
        padding-left: var(
          --jh-list-group-subheader-space-padding-left,
          var(--jh-space-600)
        );
        word-break: break-word;
      }
    `}static get properties(){return{label:{type:String},accessibleLabel:{type:String,attribute:"accessible-label"}}}constructor(){super(),this.label=null,this.accessibleLabel=null}connectedCallback(){super.connectedCallback(),this.#e=Sa++}render(){return d`
      ${this.label?d`<div class="subheader" id="list-group-labelledby-${this.#e}">
            ${this.label}
          </div>`:null}
      <div
        role="group"
        aria-labelledby=${v(this.label?`list-group-labelledby-${this.#e}`:null)}
        aria-label=${v(this.accessibleLabel)}
      >
        <slot></slot>
      </div>
    `}};customElements.define("jh-list-group",No);var Uo=class extends g{#e;static get styles(){return y`
      :host {
        background-color: var(--jh-list-item-color-background, transparent);
        padding-bottom: var(--jh-space-400);
        font: var(--jh-font-body-medium-1);
        color: var(
          --jh-list-item-color-text,
          var(--jh-color-content-primary-enabled)
        );
        display: block;
        box-sizing: border-box;
      }
      /* list item height is set to auto when styling hook is not set */
      .list-item {
        padding-right: var(
          --jh-list-item-space-padding-right,
          var(--jh-space-600)
        );
        padding-left: var(
          --jh-list-item-space-padding-left,
          var(--jh-space-600)
        );
        padding-top: var(--jh-space-400);
        display: flex;
        flex-direction: row;
        align-items: center;
        height: calc(var(--jh-list-item-size-height, auto) - var(--jh-size-400));
        box-sizing: border-box;
      }
      :host([show-divider]) .list-item {
        height: calc(var(--jh-list-item-size-height, auto) - var(--jh-size-400) - 1px);
      }

      /* states for interactive list-items*/
      :host([tabindex]:focus-visible) {
        background-color: var(
          --jh-list-item-color-background-focus,
          var(--jh-color-container-primary-hover)
        );
        outline-color: var(
          --jh-list-item-color-outline-focus,
          var(--jh-color-interactive-focus-outer)
        );
        outline-style: solid;
        outline-width: 2px;
        outline-offset: -2px;
      }
      :host([tabindex]:hover) {
        background-color: var(
          --jh-list-item-color-background-hover,
          var(--jh-color-container-primary-hover)
        );
        cursor: pointer;
      }
      :host([tabindex]:active) {
        background-color: var(
          --jh-list-item-color-background-active,
          var(--jh-color-container-primary-active)
        );
      }
      :host([tabindex][selected]) {
        background-color: var(
          --jh-list-item-color-background-selected,
          var(--jh-color-container-primary-selected)
        );
        border-left-width: 8px;
        border-left-color: var(
          --jh-list-item-color-border-selected,
          var(--jh-color-content-brand-enabled)
        );
        border-left-style: solid;
      }
      :host([tabindex][selected]) .list-item {
        padding-left: var(--jh-space-400);
      }
      :host([tabindex][disabled]) {
        opacity: var(--jh-opacity-disabled);
        cursor: default;
        pointer-events: none;
      }

      :host([show-divider]) {
        padding-bottom: 0;
      }
      jh-divider {
        margin-bottom: 0;
      }
      ::slotted([slot='jh-list-item-left']) {
        color: var(
          --jh-list-item-color-text,
          var(--jh-color-content-primary-enabled)
        );
        margin-right: var(--jh-space-200);
        flex: 0 0 auto;
      }
      ::slotted([slot='jh-list-item-content']) {
        color: var(
          --jh-list-item-color-text,
          var(--jh-color-content-primary-enabled)
        );
        flex: 1 1 auto;
      }
      .text {
        display: flex;
        flex-direction: column;
        flex: 1 1 auto;
        min-width: 0;
      }
      ::slotted([slot='jh-list-item-metadata']) {
        color: var(
          --jh-list-item-color-text,
          var(--jh-color-content-primary-enabled)
        );
        margin-left: var(--jh-space-200);
        flex: 0 0 auto;
      }
      .metadata {
        margin-left: var(--jh-space-200);
        flex: 0 0 auto;
        display: flex;
        flex-direction: column;
      }
      ::slotted([slot='jh-list-item-right']) {
        color: var(
          --jh-list-item-color-text,
          var(--jh-color-content-primary-enabled)
        );
        margin-left: var(--jh-space-200);
        flex: 0 0 auto;
      }
      .primary-text {
        color: var(
          --jh-list-item-color-text-primary,
          var(--jh-color-content-primary-enabled)
        );
      }
      .secondary-text {
        color: var(
          --jh-list-item-color-text-secondary,
          var(--jh-color-content-secondary-enabled)
        );
        font: var(--jh-font-helper-regular);
        margin-top: var(--jh-space-50);
      }

      /* text truncation */
      .primary-text,
      .secondary-text {
        display: block;
        overflow: hidden;
        white-space: nowrap;
        text-overflow: ellipsis;
      }
      .primary-metadata {
        color: var(
          --jh-list-item-metadata-color-text-primary,
          var(--jh-color-content-primary-enabled)
        );
        text-align: right;
        white-space: nowrap;
      }
      .secondary-metadata {
        color: var(
          --jh-list-item-metadata-color-text-secondary,
          var(--jh-color-content-secondary-enabled)
        );
        font: var(--jh-font-helper-regular);
        margin-top: var(--jh-space-50);
        text-align: right;
        white-space: nowrap;
      }
    `}static get properties(){return{disabled:{type:Boolean,reflect:!0},dividerInset:{type:Number,attribute:"divider-inset"},primaryMetadata:{type:String,attribute:"primary-metadata"},primaryText:{type:String,attribute:"primary-text"},secondaryMetadata:{type:String,attribute:"secondary-metadata"},secondaryText:{type:String,attribute:"secondary-text"},selected:{type:Boolean,reflect:!0},showDivider:{type:Boolean,reflect:!0,attribute:"show-divider"}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="listitem",this.disabled=!1,this.dividerInset=null,this.primaryMetadata=null,this.primaryText=null,this.secondaryMetadata=null,this.secondaryText=null,this.selected=!1,this.showDivider=!1,this.primaryTooltip=!1,this.secondaryTooltip=!1}#t(){let e=this.primaryTooltip,t=this.secondaryTooltip,o=this.shadowRoot.querySelector(".primary-text"),i=this.shadowRoot.querySelector(".secondary-text");this.primaryTooltip=o.offsetWidth<o.scrollWidth,this.secondaryTooltip=i.offsetWidth<i.scrollWidth,(e!==this.primaryTooltip||t!==this.secondaryTooltip)&&this.update()}async firstUpdated(){let e=this.shadowRoot.querySelectorAll("jh-tooltip"),t={characterData:!0,subtree:!0};await e.forEach(o=>{Promise.all(Array.from(o.children).map(i=>i.updateComplete)),new MutationObserver(this.#t.bind(this)).observe(o.children[0],t),new ResizeObserver(this.#t.bind(this)).observe(o.children[0])})}render(){let e;return this.disabled===!0&&this.hasAttribute("tabindex")?this.setAttribute("aria-disabled","true"):this.removeAttribute("aria-disabled"),this.showDivider&&(e=d`
        <jh-divider
          inset=${v(this.dividerInset?this.dividerInset:null)}
        ></jh-divider>
      `),d`
      <div class="list-item">
        <slot></slot>
        <slot name="jh-list-item-left"></slot>
        <slot name="jh-list-item-content">
          <div class="text">
            <jh-tooltip
              label=${this.primaryTooltip?this.primaryText:null}
              position="top-center"
              ><div class="primary-text">${this.primaryText}</div>
            </jh-tooltip>
            <jh-tooltip
              label=${this.secondaryTooltip?this.secondaryText:null}
              position="top-center"
              ><div class="secondary-text">${this.secondaryText}</div>
            </jh-tooltip>
          </div>
        </slot>
        <slot name="jh-list-item-metadata">
          <div class="metadata">
            <div class="primary-metadata">${this.primaryMetadata}</div>
            <div class="secondary-metadata">${this.secondaryMetadata}</div>
          </div>
        </slot>
        <slot name="jh-list-item-right"></slot>
      </div>
      ${e}
    `}};customElements.define("jh-list-item",Uo);var Mo=class extends g{static get styles(){return y`
      @keyframes fadeInUp  {
        0% {
          opacity: 0;
          transform: translateY(105%);
        }
        100% {
          opacity: 1;
          transform: translateY(0);
        }
      }
      @keyframes fadeOutDown {
        0% {
          opacity: 1;
          transform: translate3d(0, 0, 0);
        }
        100% {
          opacity: 0;
          transform: translate3d(0, 15%, 0);
        }
      }
      :host {
        z-index: var(--jh-toast-z-index, var(--jh-z-index-positive-1000));
        font: var(--jh-font-body-regular-1);
        animation: 0.75s cubic-bezier(0.1, 0.5, 0.1, 1) fadeInUp;
        animation-delay: initial;
        animation-iteration-count: 1;
        animation-fill-mode: backwards;
        animation-play-state: initial;
        opacity: 1;
        transform: translateY(0);
        transition: opacity, transform cubic-bezier(0.1, 0.5, 0.1, 1) 0.75s;
        will-change: opacity, transform;
        box-sizing: border-box;
        display: block;
        position: relative;
      }
      :host(.remove) {
        animation: 0.75s cubic-bezier(0.1, 0.5, 0.1, 1) fadeOutDown;
        animation-delay: initial;
        animation-iteration-count: 1;
        animation-fill-mode: backwards;
        animation-play-state: initial;
        transition: opacity, transform cubic-bezier(0.1, 0.5, 0.1, 1) 0.75s;
        opacity: 0;
      }
      jh-notification {
        box-shadow: var(--jh-toast-shadow, var(--jh-shadow-200));
      }
    `}static get properties(){return{appearance:{type:String,reflect:!0},dismissButtonAccessibleLabel:{type:String,attribute:"dismiss-button-accessible-label"},hideDismissButton:{type:Boolean,attribute:"hide-dismiss-button"},stacked:{type:Boolean,reflect:!0},timeout:{type:Number}}}constructor(){super(),this.timeout=5e3,this.appearance="neutral",this.dismissButtonAccessibleLabel=null,this.hideDismissButton=!1,this.stacked=!1,this.addEventListener("jh-dismiss",this.#r)}firstUpdated(){this.#e()}#e(){this.timeout>0&&setTimeout(()=>{this.#t()},this.timeout)}#t(){this.#o("jh-dismiss")}#o(e){this.dispatchEvent(new CustomEvent(e,{bubbles:!0,cancelable:!0,composed:!0}))}#r(){this.classList.add("remove"),this.onanimationend=()=>{this.remove()}}#i(){let e=this.querySelectorAll("jh-button");for(let t of e)t.getAttribute("size")!=="small"&&t.setAttribute("size","small")}render(){return d`
      <jh-notification
        appearance=${this.appearance}
        dismiss-button-accessible-label=${this.dismissButtonAccessibleLabel}
        ?hide-dismiss-button=${this.hideDismissButton}
        ?stacked=${this.stacked}>
          <slot name="jh-toast-icon" slot="jh-notification-icon"></slot>
          <slot></slot>
          <slot name="jh-toast-action" slot="jh-notification-action" @slotchange=${this.#i}>
          </slot>
          <slot name="jh-toast-dismiss-icon" slot="jh-notification-dismiss-icon">
            <jh-icon-xmark></jh-icon-xmark>
          </slot>
      </jh-notification>
    `}};customElements.define("jh-toast",Mo);var Do=class extends g{static get styles(){return y`
      :host {
        z-index: var(--jh-toast-controller-z-index, var(--jh-z-index-positive-1000));
        bottom: var(--jh-space-800);
        left: var(--jh-space-800);
        position: fixed;
      }
      ::slotted(jh-toast) {
        margin-top: var(--jh-space-200);
      }
    `}static get properties(){return{maxCount:{type:Number,attribute:"max-count"},role:{type:String}}}constructor(){super(),this.maxCount=3,this.role="status",window.addEventListener("jh-create-toast",this.#r.bind(this))}connectedCallback(){super.connectedCallback(),this.getAttribute("role")||this.setAttribute("role",this.role)}#e(){let e=this.children.length;if(e>this.maxCount){let t=e-this.maxCount;for(let o=0;o<t;o++)this.#t("jh-dismiss",this.children[o])}}#t(e,t){this.dispatchEvent(new CustomEvent(e,{bubbles:!0,cancelable:!0,composed:!0})),this.#o(t)}#o(e){e.classList.add("remove"),e.addEventListener("animationend",()=>{e.remove()})}#r(e){let t=document.createElement("jh-toast"),{toastAction:o,toastDismissIcon:i,text:a,toastIcon:s,dismissButtonAccessibleLabel:h,appearance:l,hideDismissButton:c,timeout:b,stacked:u}=e.detail;o&&(t.innerHTML+=o),i&&(t.innerHTML+=i),a&&(t.innerHTML+=a),s&&(t.innerHTML+=s),h&&t.setAttribute("dismiss-button-accessible-label",h),l&&t.setAttribute("appearance",l),c&&t.setAttribute("hide-dismiss-button",""),b>=0&&t.setAttribute("timeout",b),u&&t.setAttribute("stacked",""),this.appendChild(t)}render(){return d`
      <slot @slotchange=${this.#e}></slot>
    `}};customElements.define("jh-toast-controller",Do);var Li={ATTRIBUTE:1,CHILD:2,PROPERTY:3,BOOLEAN_ATTRIBUTE:4,EVENT:5,ELEMENT:6},Ri=r=>(...e)=>({_$litDirective$:r,values:e}),ft=class{constructor(e){}get _$AU(){return this._$AM._$AU}_$AT(e,t,o){this._$Ct=e,this._$AM=t,this._$Ci=o}_$AS(e,t){return this.update(e,t)}update(e,t){return this.render(...t)}};var Ve=class extends ft{constructor(e){if(super(e),this.it=p,e.type!==Li.CHILD)throw Error(this.constructor.directiveName+"() can only be used in child bindings")}render(e){if(e===p||e==null)return this._t=void 0,this.it=e;if(e===D)return e;if(typeof e!="string")throw Error(this.constructor.directiveName+"() called with a non-string value");if(e===this.it)return this._t;this.it=e;let t=[e];return t.raw=t,this._t={_$litType$:this.constructor.resultType,strings:t,values:[]}}};Ve.directiveName="unsafeHTML",Ve.resultType=1;var Pi=Ri(Ve);var Oi=Symbol(),O=class{get taskComplete(){return this.t||(this.i===1?this.t=new Promise((e,t)=>{this.o=e,this.h=t}):this.i===3?this.t=Promise.reject(this.l):this.t=Promise.resolve(this.u)),this.t}constructor(e,t,o){this.p=0,this.i=0,(this._=e).addController(this);let i=typeof t=="object"?t:{task:t,args:o};this.v=i.task,this.j=i.args,this.m=i.argsEqual??Ni,this.k=i.onComplete,this.A=i.onError,this.autoRun=i.autoRun??!0,"initialValue"in i&&(this.u=i.initialValue,this.i=2,this.O=this.T?.())}hostUpdate(){this.autoRun===!0&&this.S()}hostUpdated(){this.autoRun==="afterUpdate"&&this.S()}T(){if(this.j===void 0)return;let e=this.j();if(!Array.isArray(e))throw Error("The args function must return an array");return e}async S(){let e=this.T(),t=this.O;this.O=e,e===t||e===void 0||t!==void 0&&this.m(t,e)||await this.run(e)}async run(e){let t,o;e??=this.T(),this.O=e,this.i===1?this.q?.abort():(this.t=void 0,this.o=void 0,this.h=void 0),this.i=1,this.autoRun==="afterUpdate"?queueMicrotask(()=>this._.requestUpdate()):this._.requestUpdate();let i=++this.p;this.q=new AbortController;let a=!1;try{t=await this.v(e,{signal:this.q.signal})}catch(s){a=!0,o=s}if(this.p===i){if(t===Oi)this.i=0;else{if(a===!1){try{this.k?.(t)}catch{}this.i=2,this.o?.(t)}else{try{this.A?.(o)}catch{}this.i=3,this.h?.(o)}this.u=t,this.l=o}this._.requestUpdate()}}abort(e){this.i===1&&this.q?.abort(e)}get value(){return this.u}get error(){return this.l}get status(){return this.i}render(e){switch(this.i){case 0:return e.initial?.();case 1:return e.pending?.();case 2:return e.complete?.(this.value);case 3:return e.error?.(this.error);default:throw Error("Unexpected status: "+this.i)}}},Ni=(r,e)=>r===e||r.length===e.length&&r.every((t,o)=>!Ke(t,e[o]));var Ho={live:["pending","claimed","waiting","handedoff"],ready:["pending","handedoff"],blocked:["pending","handedoff","waiting"],pending:["pending"],claimed:["claimed"],waiting:["waiting"],handedoff:["handedoff"],parked:["parked"],done:["done"],all:[]},Ea={ready:r=>!!r.ready,blocked:r=>(r.blocked_by?.length??0)>0},Ui=[{key:"state",label:"State",get:r=>r.state},{key:"work",label:"Work",get:r=>E(r.task)||r.id},{key:"why",label:"Blocked by",get:r=>Dt(r)},{key:"stage",label:"Stage",get:r=>r.stage??""},{key:"holder",label:"Held by",get:r=>r.runner??""},{key:"origin",label:"Origin",get:r=>ee(r)},{key:"age",label:"Age",get:r=>r.age_sec??0}],yt=r=>n`<jh-tag size="small" class=${qr[r]??"neutral"} label=${r}></jh-tag>`,Ko=r=>n`<jh-tag size="small" class=${Vr(r)} label=${r}></jh-tag>`,Y=r=>n`<jh-progress accessible-label=${`Loading ${r}`}></jh-progress>`,he=r=>n`<jh-notification appearance="negative" type="alert" hide-dismiss-button>${r.message}</jh-notification>`,Ca=$`
  .chips { display: flex; gap: var(--jh-space-100); flex-wrap: wrap; }
  .chips button {
    font: var(--jh-font-helper-medium); cursor: pointer; padding: 4px var(--jh-space-300);
    border-radius: var(--jh-border-radius-pill); border: 1px solid var(--jh-color-control-enabled);
    background: transparent; color: var(--jh-color-content-primary-enabled);
  }
  .chips button:hover { background: var(--jh-color-container-secondary-hover); }
  .chips button[aria-pressed="true"] { background: var(--jh-color-container-secondary-selected); border-color: transparent; font-weight: 600; }
`,Mi=$`
  dl.facts { display: grid; grid-template-columns: max-content 1fr; gap: var(--jh-space-100) var(--jh-space-400); margin: 0; }
  dl.facts dt { font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
  dl.facts dd { margin: 0; overflow-wrap: anywhere; }
  @media (max-width: 560px) { dl.facts { grid-template-columns: 1fr; } dl.facts dd { margin-bottom: var(--jh-space-200); } }
`,Io=class extends j{static styles=[C,Mi,$`
    :host { display: grid; gap: var(--jh-space-400); }
    .tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: var(--jh-space-300); margin-bottom: var(--jh-space-400); }
    .tile { display: block; padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200);
            background: var(--jh-color-container-secondary-enabled); color: inherit; text-decoration: none; }
    a.tile:hover { background: var(--jh-color-container-secondary-hover); }
    .tile .n { font: var(--jh-font-heading-medium-4); }
    .tile .l { font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    .tile .s { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .tile.negative .n { color: var(--jh-color-content-negative-enabled); }
    .tile.positive .n { color: var(--jh-color-content-positive-enabled); }
    .foot { display: flex; align-items: center; gap: var(--jh-space-300); flex-wrap: wrap; margin-top: var(--jh-space-400);
            padding-top: var(--jh-space-300); border-top: 1px solid var(--jh-color-divider-secondary); }
    ol.feed { list-style: none; margin: 0; padding: 0; max-height: 480px; overflow-y: auto; }
    ol.feed li { display: grid; grid-template-columns: minmax(92px, max-content) 1fr max-content; gap: var(--jh-space-300);
                 align-items: baseline; padding: var(--jh-space-200) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    ol.feed .what { overflow-wrap: anywhere; }
    ol.feed .by { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    ol.feed .t { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); white-space: nowrap; }
    .row { display: flex; gap: var(--jh-space-300); align-items: center; flex-wrap: wrap; margin-bottom: var(--jh-space-300); }
  `];_status=new O(this,{task:async(e,{signal:t})=>ze(t),args:()=>[]});_items=new O(this,{task:async(e,{signal:t})=>Rt([],t),args:()=>[]});_activity=new N(this,{onEvent:()=>this._refresh()});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>{this._status.run(),this._items.run()},400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}async _reload(){try{let e=await Nr();R(`Configuration reloaded: ${e.reloaded}`,"positive")}catch(e){R(e.message,"negative",1e4)}}_tile(e,t,{href:o,sub:i,tone:a}={}){let s=n`<div class="n">${e}</div><div class="l">${t}</div>${i?n`<div class="s">${i}</div>`:p}`;return o?n`<a class=${"tile "+(a??"")} href=${o}>${s}</a>`:n`<div class=${"tile "+(a??"")}>${s}</div>`}_statusCard(e){let t=e.exception_queue,o=e.spend;return n`
      <div class="tiles">
        ${this._tile(e.pending,"pending",{href:"/work",sub:"owed a run"})}
        ${this._tile(e.exceptions,"exceptions",{href:"/exceptions",sub:"waiting on a person",tone:e.exceptions?"negative":""})}
        ${t?.parked?this._tile(k(t.oldest_age_sec),"oldest parked",{href:"/exceptions",sub:`objective ${k(t.objective_sec)}`,tone:t.met?"":"negative"}):p}
        ${this._tile(e.claims?.stopped?"Stopped":"Running","claims",{sub:e.claims?.actor?`by ${e.claims.actor}`:"never touched",tone:e.claims?.stopped?"negative":"positive"})}
        ${o?this._tile(`${o.running}/${o.max_concurrent}`,"running now",{href:"/sessions",sub:o.started?`${o.started} started this window`:""}):p}
        ${o?this._tile(W(o.tokens),"tokens",{sub:o.max_tokens?`of ${W(o.max_tokens)} this window`:"this window"}):p}
        ${o&&(o.cost_micros||o.max_cost_micros)?this._tile(Je(o.cost_micros),"spend",{sub:o.max_cost_micros?`of ${Je(o.max_cost_micros)}`:"this window"}):p}
      </div>
      <dl class="facts">
        <dt>Queue</dt>
        <dd>${e.backlog?.total?n`${Br(e.backlog.by_state)} · <a href="/work">list them</a>`:"empty"}</dd>
        ${o&&et(o)?n`<dt>Tokens</dt><dd>${et(o)}</dd>`:p}
        <dt>Routes</dt>
        <dd>${e.routes?.length?n`<jh-tag-group>${e.routes.map(i=>n`<jh-tag size="small" label=${i} href="/submit"></jh-tag>`)}</jh-tag-group>`:n`<span class="muted">none configured — nothing can be submitted</span>`}</dd>
        <dt>Daemon</dt>
        <dd>${e.runner} ${e.version}, up ${k(e.uptime_sec)}, serving tenant <b>${e.tenant}</b> box <b>${e.box}</b></dd>
      </dl>
      ${""}
      ${o?.unmetered?n`
        <jh-notification appearance="negative" type="alert" hide-dismiss-button>
          ${o.unmetered} run(s) this window did not report a cost — the spend above is incomplete and claims are stopped until the window turns.
        </jh-notification>`:p}
      ${o?.unpriced?n`
        <jh-notification appearance="negative" type="alert" hide-dismiss-button>
          ${o.unpriced} run(s) this window reported tokens but no cost — the money figure above is incomplete and claims are stopped until the window turns.
        </jh-notification>`:p}
      ${o?.refusing&&!o.unmetered&&!o.unpriced?n`
        <jh-notification appearance="neutral" type="alert" hide-dismiss-button>
          The meter is pacing work: the factory is idle because it is over budget, not because there is nothing to do.
        </jh-notification>`:p}
      ${t&&!t.met?n`
        <jh-notification appearance="negative" type="alert" hide-dismiss-button>
          <span>${t.overdue} exception(s) have waited longer than the ${k(t.objective_sec)} response-time objective — the oldest for ${k(t.oldest_age_sec)}. <a href="/exceptions">Work the queue</a></span>
        </jh-notification>`:p}
      <div class="foot">
        <jh-button appearance="tertiary" size="small" label="Reload configuration" @click=${this._reload}>
          <span slot="jh-button-icon-left">${Z("rotate","x-small")}</span>
        </jh-button>
        <span class="helper">Re-reads box.yaml on the running daemon. Routes, executors and budget swap hot; tenant, store and socket are identity and are refused.</span>
      </div>`}_inFlight(e){let t=(e??[]).filter(o=>Ho.live.includes(o.state));return t.length===0?n`<p class="empty">Nothing in flight.</p>`:n`
      <jh-list-group accessible-label="In-flight work">
        ${t.map(o=>n`
          <jh-list-item
            primary-text=${E(o.task)||o.id}
            secondary-text=${o.note?w(o.note):ee(o)||te(o)}
            primary-metadata=${o.stage??""}
            secondary-metadata=${o.stage?`${k(o.in_state_sec)} in stage`:`${k(o.age_sec)} old`}
            show-divider>
            <span slot="jh-list-item-left">${yt(o.state)}</span>
            <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
              label="Open" accessible-label=${`Open ${E(o.task)||o.id}`} href=${"/items/"+o.id}></jh-button>
          </jh-list-item>`)}
      </jh-list-group>`}_feed(e){let t=this._activity.events.slice().reverse();return t.length===0?n`<p class="empty">Nothing yet.</p>`:n`
      <ol class="feed" aria-label="Activity feed">
        ${t.map(o=>{let i=It(o);return n`
            <li>
              ${Ko(o.kind)}
              <div>
                <div class="what">${ot(o,e)}${i?n` · <a href=${"/items/"+i}>open</a>`:p}</div>
                <div class="by">${o.actor}${o.scope&&!i?n` · ${o.scope}`:p}</div>
              </div>
              <span class="t"><box-when .at=${o.at}></box-when></span>
            </li>`})}
      </ol>`}render(){let e=Object.fromEntries((this._items.value??[]).map(t=>[t.id,t.task]));return n`
      <jh-card header-title="Factory status" header-subtitle="The numbers an operator watches, and what each is made of" padding="medium">
        ${this._status.render({pending:()=>Y("status"),error:he,complete:t=>this._statusCard(t)})}
      </jh-card>
      <jh-card header-title="In flight" header-subtitle="What the factory is doing right now, unit by unit" padding="medium">
        ${this._items.render({pending:()=>Y("in-flight work"),error:he,complete:t=>this._inFlight(t)})}
      </jh-card>
      <jh-card header-title="Activity" header-subtitle="Everything the factory does, as it does it" padding="medium">
        ${this._feed(e)}
      </jh-card>
    `}};customElements.define("box-dashboard",Io);var Bo=class extends j{static styles=[C,Ca,$`
    :host { display: block; }
    .bar { display: flex; align-items: center; gap: var(--jh-space-300); flex-wrap: wrap; margin-bottom: var(--jh-space-300); }
    .wrap { overflow-x: auto; }
    table { border-collapse: collapse; width: 100%; min-width: 820px; table-layout: fixed; }
    caption { text-align: left; padding-bottom: var(--jh-space-200); font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    th, td { text-align: left; vertical-align: top; padding: var(--jh-space-200) var(--jh-space-300) var(--jh-space-200) 0;
             border-bottom: 1px solid var(--jh-color-divider-secondary); }
    thead th { white-space: nowrap; font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    thead th button { font: inherit; cursor: pointer; padding: 0; border: 0; background: transparent; color: inherit; }
    thead th[aria-sort="ascending"] button::after { content: ' \\2191'; }
    thead th[aria-sort="descending"] button::after { content: ' \\2193'; }
    tbody th { font-weight: 500; }
    tbody th a { text-decoration: none; }
    tbody th a:hover { text-decoration: underline; }
    .note { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
            font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); margin-top: 2px; }
    .stack { display: grid; gap: 4px; justify-items: start; }
    .ready { font: var(--jh-font-micro-medium); color: var(--jh-color-content-positive-enabled); text-transform: uppercase; }
    .lapsed { color: var(--jh-color-content-negative-enabled); font-weight: 600; }
    .nowrap { white-space: nowrap; }
  `];static properties={_filter:{state:!0},_sort:{state:!0},_desc:{state:!0}};constructor(){super(),this._filter="live",this._sort="",this._desc=!1,this._activity=new N(this,{limit:1,onEvent:()=>this._refresh()})}_task=new O(this,{task:async([e],{signal:t})=>Rt(Ho[e],t),args:()=>[this._filter]});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>this._task.run(),400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}_sortBy(e){if(this._sort===e){this._desc=!this._desc;return}this._sort=e,this._desc=!1}_rows(e){let t=Ea[this._filter],o=(e??[]).filter(s=>t?t(s):!0),i=Ui.find(s=>s.key===this._sort);if(!i)return o;let a=this._desc?-1:1;return o.slice().sort((s,h)=>{let l=i.get(s),c=i.get(h);return typeof l=="number"&&typeof c=="number"?(l-c)*a:String(l).localeCompare(String(c))*a})}render(){return n`
      <jh-card header-title="Work" header-subtitle="The frontier: what is runnable, what is blocked, and on what" padding="medium">
        <div class="bar">
          <div class="chips" role="group" aria-label="Show">
            ${Object.keys(Ho).map(e=>n`
              <button type="button" aria-pressed=${this._filter===e?"true":"false"}
                @click=${()=>{this._filter=e}}>${e}</button>`)}
          </div>
        </div>
        ${this._task.render({pending:()=>Y("work"),error:he,complete:e=>this._table(this._rows(e))})}
      </jh-card>`}_table(e){return e.length===0?n`<p class="empty">Nothing in this lane.</p>`:n`
      <div class="wrap">
        <table>
          <caption>${e.length} item(s), ${this._sort?`sorted by ${this._sort}`:"oldest first"}</caption>
          <colgroup>
            <col style="width: 108px"><col><col style="width: 150px"><col style="width: 110px">
            <col style="width: 160px"><col style="width: 150px"><col style="width: 64px">
          </colgroup>
          <thead>
            <tr>${Ui.map(t=>n`
              <th scope="col" aria-sort=${this._sort===t.key?this._desc?"descending":"ascending":"none"}>
                <button type="button" @click=${()=>this._sortBy(t.key)}>${t.label}</button>
              </th>`)}
            </tr>
          </thead>
          <tbody>
            ${e.map(t=>n`
              <tr>
                <td><div class="stack">${yt(t.state)}${t.ready?n`<span class="ready">ready</span>`:p}</div></td>
                <th scope="row">
                  <a href=${"/items/"+t.id}>${E(t.task)||t.id}</a>
                  ${t.note?n`<div class="note" title=${t.note}>${w(t.note)}</div>`:p}
                </th>
                <td>${t.blocked_by?.length?t.blocked_by.map((o,i)=>n`${i?", ":""}<a href=${"/items/"+o}>${I(o)}</a>`):Dt(t)||n`<span class="muted">—</span>`}</td>
                <td class="nowrap">${t.stage??""}
                  ${t.stage?n`<div class="helper">${k(t.in_state_sec)}</div>`:p}</td>
                <td class=${t.lease_lapsed?"lapsed":""}>${tt(t)||n`<span class="muted">—</span>`}</td>
                <td class="muted">${ee(t)||te(t)}</td>
                <td class="nowrap">${k(t.age_sec)}</td>
              </tr>`)}
          </tbody>
        </table>
      </div>`}};customElements.define("box-work",Bo);function za(r){switch(r.kind){case"started":return"agent started";case"tool":return r.tool||"(unnamed \u2014 nothing for policy to match)";case"denied":return`${r.tool} \u2014 refused by policy at the stream`;case"escalate":return r.reason;case"handoff":return r.reason;default:return r.text??""}}var Ta={denied:"negative",escalate:"negative",handoff:"neutral",started:"positive"},qo=class extends j{static styles=[C,$`
    :host { display: block; }
    .grid { display: grid; gap: var(--jh-space-400); grid-template-columns: repeat(auto-fit, minmax(380px, 1fr)); align-items: start; }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; font: var(--jh-font-helper-regular);
            color: var(--jh-color-content-secondary-enabled); margin-bottom: var(--jh-space-200); }
    .layers { display: flex; gap: var(--jh-space-100); flex-wrap: wrap; margin-bottom: var(--jh-space-200); }
    .layer { font: var(--jh-font-micro-medium); padding: 2px var(--jh-space-200); border-radius: var(--jh-border-radius-100);
             background: var(--jh-color-container-secondary-enabled); }
    h4 { margin: var(--jh-space-300) 0 var(--jh-space-100); font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    ol { list-style: none; margin: 0; padding: 0; max-height: 320px; overflow-y: auto; font: var(--jh-font-helper-regular); }
    li { display: grid; grid-template-columns: 64px 72px 1fr; gap: var(--jh-space-200); padding: 3px 0;
         border-bottom: 1px solid var(--jh-color-divider-secondary); }
    li .t { color: var(--jh-color-content-secondary-enabled); font-variant-numeric: tabular-nums; }
    li .k { font-weight: 600; }
    li .x { white-space: pre-wrap; overflow-wrap: anywhere; }
    li.negative .k { color: var(--jh-color-content-negative-enabled); }
    li.positive .k { color: var(--jh-color-content-positive-enabled); }
    .blind { color: var(--jh-color-content-secondary-enabled); font-style: italic; margin: 0; }
  `];static properties={_frames:{state:!0}};constructor(){super(),this._frames={},this._activity=new N(this,{limit:1,onEvent:()=>this._refresh(),onSession:e=>{this._frames={...this._frames,[e.item]:[...this._frames[e.item]??[],e]}}})}_task=new O(this,{task:async(e,{signal:t})=>Tr(t),args:()=>[]});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>this._task.run(),400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}_timeline(e){let t=new Set((e.timeline??[]).map(o=>o.seq));return[...e.timeline??[],...(this._frames[e.id]??[]).filter(o=>!t.has(o.seq))]}render(){return this._task.render({pending:()=>n`<jh-card header-title="Sessions" padding="medium">${Y("sessions")}</jh-card>`,error:e=>n`<jh-card header-title="Sessions" padding="medium">${he(e)}</jh-card>`,complete:e=>(e?.length??0)===0?n`<jh-card header-title="Sessions" header-subtitle="Each running unit, and what its agent is doing right now" padding="medium">
            <p class="empty">No unit is running. Claimed work appears here as it starts.</p>
          </jh-card>`:n`<div class="grid">${e.map(t=>this._card(t))}</div>`})}_card(e){let t=this._timeline(e);return n`
      <jh-card header-title=${E(e.task)||e.id} header-subtitle=${ee(e)||te(e)} padding="medium">
        <div class="meta">
          ${yt(e.state)}
          ${e.stage?n`<jh-tag size="small" label=${e.stage}></jh-tag><span>${k(e.in_state_sec)} in stage</span>`:p}
          ${e.seat?n`<span>seat <b>${e.seat}</b></span>`:p}
          ${e.runner?n`<span>${tt(e)}</span>`:p}
        </div>
        <div class="meta">
          <span>item <box-hash label="item id" .value=${e.id} href=${"/items/"+e.id} chars="18"></box-hash></span>
          ${e.agentref?n`<span>agent <box-hash label="agentref" .value=${e.agentref}></box-hash></span>`:p}
          ${e.head?n`<span>head <box-hash label="commit" .value=${e.head} href=${"/provenance/"+e.head}></box-hash></span>`:p}
        </div>
        ${""}
        ${e.layers?.length?n`<div class="layers" aria-label="Agent layers">${e.layers.map(o=>n`
              <span class="layer" title=${`${o.source} ${o.digest}`}>${o.scope}:${o.name}</span>`)}</div>`:p}
        <h4>Timeline</h4>
        ${t.length===0?e.watching?n`<p class="empty">Started; nothing said yet.</p>`:n`<p class="blind">No live stream here — another runner holds this, or the window has rolled.
                     Silence about the run, not an idle agent.</p>`:n`<ol aria-label="Agent timeline">${t.map(o=>n`
              <li class=${Ta[o.kind]??""}>
                <span class="t"><box-when .at=${o.at}></box-when></span>
                <span class="k">${o.kind}</span>
                <span class="x">${za(o)}</span>
              </li>`)}
            </ol>`}
      </jh-card>`}};customElements.define("box-sessions",qo);var Vo=class extends j{static styles=[C,$`
    :host { display: grid; gap: var(--jh-space-400); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; font: var(--jh-font-helper-regular);
            color: var(--jh-color-content-secondary-enabled); margin-bottom: var(--jh-space-300); }
    .overdue { color: var(--jh-color-content-negative-enabled); font-weight: 600; }
    .note { margin: 0 0 var(--jh-space-300); padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200);
            background: var(--jh-color-container-secondary-enabled); white-space: pre-wrap; overflow-wrap: anywhere; }
    .summary { display: flex; gap: var(--jh-space-400); flex-wrap: wrap; align-items: center; }
  `];_task=new O(this,{task:async(e,{signal:t})=>kr(t),args:()=>[]});constructor(){super(),this._activity=new N(this,{limit:1,onEvent:e=>{/index\/exceptions|workitem\//.test(e.scope)&&this._refresh()}})}_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>this._task.run(),400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}_done(e,t){R(`${e}: ${E(t.task)||t.id}`,"positive"),this._refresh()}render(){return this._task.render({pending:()=>n`<jh-card header-title="Exception queue" padding="medium">${Y("exceptions")}</jh-card>`,error:e=>n`<jh-card header-title="Exception queue" padding="medium">${he(e)}</jh-card>`,complete:e=>n`
        <jh-card header-title="Exception queue" header-subtitle="Parked work waiting on a person, loudest and oldest first" padding="medium">
          ${(e?.length??0)===0?n`<p class="empty">Nothing here — the factory has nothing waiting on a person.</p>`:n`<div class="summary">
                <span><b>${e.length}</b> parked</span>
                <span class=${e.some(t=>t.overdue)?"overdue":""}><b>${e.filter(t=>t.overdue).length}</b> past the response-time objective</span>
                <span class="helper">Retry hands the unit back to the factory. Resolve closes it with your reason on the record.</span>
              </div>`}
        </jh-card>
        ${(e??[]).map(t=>this._card(t))}`})}_card(e){return n`
      <jh-card header-title=${E(e.task)||e.id} header-subtitle=${ee(e)||te(e)} padding="medium">
        <div class="meta">
          <jh-tag size="small" class=${Ht[e.severity??"medium"]??"neutral"} label=${`severity ${e.severity??"medium"}`}></jh-tag>
          ${e.stage?n`<jh-tag size="small" label=${`at ${e.stage}`}></jh-tag>`:p}
          <span>parked <box-when .at=${e.parked}></box-when></span>
          ${e.overdue?n`<span class="overdue">past the response-time objective</span>`:p}
          ${e.escalations?n`<span>escalated out of band ${e.escalations}×</span>`:p}
          <span>item <box-hash label="item id" .value=${e.id} href=${"/items/"+e.id} chars="18"></box-hash></span>
        </div>
        ${e.note?n`<p class="note">${e.note}</p>`:p}
        <div class="actions">
          <box-reason label="Retry" confirm="Retry" appearance="primary" glyph="rotate" prompt=${`Retry ${E(e.task)}`}
            .action=${t=>Ot(e.id,t)} @box-done=${()=>this._done("Retried",e)}></box-reason>
          <box-reason label="Resolve" confirm="Resolve" appearance="secondary" glyph="check" prompt=${`Resolve ${E(e.task)}`}
            .action=${t=>Pt(e.id,t)} @box-done=${()=>this._done("Resolved",e)}></box-reason>
          <jh-button appearance="tertiary" size="small" label="Open" accessible-label=${`Open ${E(e.task)||e.id}`} href=${"/items/"+e.id}></jh-button>
        </div>
      </jh-card>`}};customElements.define("box-exceptions",Vo);var Zo=class extends j{static properties={itemId:{attribute:!1}};static styles=[C,Mi,$`
    :host { display: grid; gap: var(--jh-space-400); }
    .meta { display: flex; gap: var(--jh-space-200); flex-wrap: wrap; align-items: center; margin-bottom: var(--jh-space-300); }
    .note { margin: var(--jh-space-300) 0 0; white-space: pre-wrap; overflow-wrap: anywhere; }
    details { margin-top: var(--jh-space-300); }
    summary { cursor: pointer; font: var(--jh-font-helper-medium); color: var(--jh-color-content-secondary-enabled); }
    pre { white-space: pre-wrap; overflow-wrap: anywhere; font: var(--jh-font-code-regular-2); margin: var(--jh-space-200) 0 0;
          padding: var(--jh-space-300); border-radius: var(--jh-border-radius-200); background: var(--jh-color-container-secondary-enabled); }
    .actions { margin-top: var(--jh-space-400); }
    .lapsed { color: var(--jh-color-content-negative-enabled); font-weight: 600; }
    ol.chain { list-style: none; margin: 0; padding: 0; }
    ol.chain li { display: grid; grid-template-columns: minmax(92px, max-content) 1fr max-content; gap: var(--jh-space-300);
                  align-items: baseline; padding: var(--jh-space-200) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    ol.chain .what { overflow-wrap: anywhere; }
    ol.chain .by { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    ol.chain .t { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); white-space: nowrap; }
  `];constructor(){super(),this._activity=new N(this,{limit:1,onEvent:e=>{(e.scope===`workitem/${this.itemId}`||e.subject===this.itemId)&&this._refresh()}})}_task=new O(this,{task:async([e],{signal:t})=>{let[o,i,a]=await Promise.all([Ar(e,t),Lr(e,t).catch(()=>({})),Sr(e,t)]);return{item:{...i,...o,note:o.note??i.note},evidence:a}},args:()=>[this.itemId]});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>this._task.run(),400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}_done(e,t){R(`${e}: ${E(t.task)||t.id}`,"positive"),this._refresh()}_head(e){let t=Hr(e.task),o=e.state==="parked"||e.state==="failed",i=o?"negative":e.state==="done"?"positive":"neutral";return n`
      <jh-card header-title=${E(e.task)||e.id} header-subtitle=${ee(e)||te(e)} padding="medium">
        <div class="meta">
          ${yt(e.state)}
          ${e.stage&&e.stage!==e.state?n`<jh-tag size="small" label=${`at ${e.stage}`}></jh-tag>`:p}
          ${e.severity?n`<jh-tag size="small" class=${Ht[e.severity]??"neutral"} label=${`severity ${e.severity}`}></jh-tag>`:p}
          ${t?n`<jh-button appearance="tertiary" size="small" label=${/github\.com/.test(t)?"Open on GitHub":"Open link"} href=${t} target="_blank">
              <span slot="jh-button-icon-right">${Z("external","x-small")}</span>
            </jh-button>`:p}
        </div>
        ${e.note?n`
          <jh-notification appearance=${i} type="alert" hide-dismiss-button>
            <div class="note">${e.note}</div>
          </jh-notification>`:p}
        <dl class="facts" style="margin-top: var(--jh-space-400)">
          <dt>Item</dt><dd><box-hash label="item id" .value=${e.id} chars="24"></box-hash></dd>
          <dt>Where</dt><dd>${te(e)||n`<span class="muted">—</span>`}${e.requester?n` · requested by ${e.requester}`:p}</dd>
          <dt>Ingress</dt><dd><box-hash label="ingress id" .value=${e.ingress} chars="24"></box-hash></dd>
          ${e.head?n`<dt>Head</dt><dd><box-hash label="commit" .value=${e.head} href=${"/provenance/"+e.head}></box-hash> <span class="helper">landed commit — its provenance</span></dd>`:p}
          ${e.built&&e.built!==e.head?n`<dt>Built</dt><dd><box-hash label="commit" .value=${e.built} href=${"/provenance/"+e.built}></box-hash></dd>`:p}
          ${e.seat?n`<dt>Seat</dt><dd>${e.seat}${e.agentref?n` · agent <box-hash label="agentref" .value=${e.agentref}></box-hash>`:p}</dd>`:p}
          ${e.runner?n`<dt>Held by</dt><dd class=${e.lease_lapsed?"lapsed":""}>${tt(e)}</dd>`:p}
          ${e.blocked_by?.length?n`<dt>Blocked by</dt><dd>${e.blocked_by.map((a,s)=>n`${s?", ":""}<a href=${"/items/"+a}>${a}</a>`)}</dd>`:p}
          ${e.root?n`<dt>Cascaded from</dt><dd><a href=${"/items/"+e.root}>${e.root}</a></dd>`:p}
          ${e.depends_on?.length?n`<dt>Depends on</dt><dd>${e.depends_on.map((a,s)=>n`${s?", ":""}<a href=${"/items/"+a}>${a}</a>`)}</dd>`:p}
          ${e.prior?n`<dt>Prior attempt</dt><dd>carried into this run</dd>`:p}
          ${e.opened?n`<dt>Opened</dt><dd><box-when .at=${e.opened} abs></box-when> <span class="helper">(<box-when .at=${e.opened}></box-when>)</span></dd>`:p}
          ${e.changed?n`<dt>Last change</dt><dd><box-when .at=${e.changed} abs></box-when> <span class="helper">(${k(e.in_state_sec)} in this state)</span></dd>`:p}
          ${e.hops?n`<dt>Sessions</dt><dd>${e.hops+1}, after ${e.hops} handoff(s)</dd>`:p}
        </dl>
        <details>
          <summary>Full task text</summary>
          <pre>${e.task}</pre>
        </details>
        <div class="actions">
          ${o?n`
            <box-reason label="Retry" confirm="Retry" appearance="primary" glyph="rotate" prompt="Retry this item"
              .action=${a=>Ot(e.id,a)} @box-done=${()=>this._done("Retried",e)}></box-reason>
            <box-reason label="Resolve" confirm="Resolve" appearance="secondary" glyph="check" prompt="Resolve this item"
              .action=${a=>Pt(e.id,a)} @box-done=${()=>this._done("Resolved",e)}></box-reason>
            <span class="helper">Retry hands the unit back to the factory; resolve closes it with your reason on the record.</span>`:e.state==="claimed"?n`<jh-button appearance="tertiary" size="small" label="Watch the session" href="/sessions"></jh-button>
                     <span class="helper">A running unit can only be stopped with the rest of the factory — the cord is at the top of the page.</span>`:n`<span class="helper">Retry and resolve apply to parked or failed work.</span>`}
        </div>
      </jh-card>`}_chain(e){return(e?.length??0)===0?n`<p class="empty">Nothing recorded yet.</p>`:n`
      <ol class="chain" aria-label="Run timeline">
        ${e.map(t=>n`
          <li>
            ${Ko(t.Kind)}
            <div>
              <div class="what">${ot(t)}</div>
              <div class="by">${t.Actor}${t.Subject&&t.Subject!==this.itemId?n` · ${I(t.Subject)}`:p}</div>
            </div>
            <span class="t"><box-when .at=${t.At}></box-when></span>
          </li>`)}
      </ol>`}render(){return this._task.render({pending:()=>Y("item"),error:he,complete:({item:e,evidence:t})=>n`
        ${this._head(e)}
        <jh-card header-title="Timeline" header-subtitle="What this unit did, in order, from its own evidence chain" padding="medium">
          ${this._chain(t)}
        </jh-card>`})}};customElements.define("box-item",Zo);var Wo=class extends j{static properties={sha:{attribute:!1}};static styles=[C,$`
    :host { display: block; }
    .sha { margin: 0 0 var(--jh-space-300); }
    .diagram { overflow-x: auto; margin: var(--jh-space-300) 0; color: var(--jh-color-content-primary-enabled); }
    .gap { margin-bottom: var(--jh-space-300); }
    .span { margin: var(--jh-space-400) 0 0; }
    .span h3 { margin: 0 0 var(--jh-space-100); font: var(--jh-font-body-md-semibold); }
    .verify { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .verify.broken { color: var(--jh-color-content-danger-enabled); font-weight: 600; }
    .worm { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    ol { list-style: none; margin: 0; padding: 0; }
    li { display: grid; grid-template-columns: minmax(92px, max-content) 1fr max-content; gap: var(--jh-space-300);
         align-items: baseline; padding: var(--jh-space-200) 0; border-bottom: 1px solid var(--jh-color-divider-secondary); }
    li.context { opacity: 0.65; }
    .by { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); }
    .t { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); white-space: nowrap; }
    .actions { margin-top: var(--jh-space-400); }
  `];_task=new O(this,{task:async([e],{signal:t})=>e?Er(e,t):null,args:()=>[this.sha]});_span(e){let t=new Set(e.names??[]);return n`
      <section class="span">
        <h3>${e.scope}</h3>
        ${e.verify?n`<p class="verify broken">Chain broken: ${e.verify}</p>`:e.whole?n`<p class="verify">${e.total} record(s) — chain verifies end to end.</p>`:n`<p class="verify">Standing scope — ${e.total} record(s) verify; showing the ${e.records?.length??0} naming this commit.</p>`}
        ${e.compacted?n`<p class="worm">Crosses into WORM: records ${e.compacted.from}–${e.compacted.through} at ${e.compacted.export?.uri}${e.compacted.export?.retain_until&&e.compacted.export.retain_until!=="0001-01-01T00:00:00Z"?n`, retention locked until <box-when .at=${e.compacted.export.retain_until} abs></box-when>`:", no retention lock (local sink)"}</p>`:p}
        <ol aria-label=${`Records in ${e.scope}`}>
          ${(e.records??[]).map(o=>{let i=It(o);return n`
              <li class=${t.has(o.Seq)?"":"context"}>
                ${Ko(o.Kind)}
                <div>
                  <div>${ot(o)}${i?n` · <a href=${"/items/"+i}>open the item</a>`:p}</div>
                  <div class="by">${o.Actor}${o.OnBehalfOf?` for ${o.OnBehalfOf}`:""} · ${o.Scope} #${o.Seq}${t.has(o.Seq)?" \xB7 names this commit":""}</div>
                </div>
                <span class="t"><box-when .at=${o.At}></box-when></span>
              </li>`})}
        </ol>
      </section>`}render(){return n`
      <jh-card header-title="Provenance" header-subtitle="The chain this commit is a link of — every span verified, every gap named" padding="medium">
        <p class="sha">Commit <box-hash label="commit" .value=${this.sha} chars="16"></box-hash></p>
        ${this._task.render({pending:()=>Y("provenance"),error:he,complete:e=>{let t=e?.chain??{};return(t.spans?.length??0)===0?n`<p class="empty">Nothing is recorded about this commit. Either it did not come through this factory, or it has not landed yet.</p>`:n`
              ${(t.gaps??[]).map(o=>n`
                <jh-notification class="gap" appearance="negative" type="alert" hide-dismiss-button>
                  <strong>Missing ${o.link}</strong> — ${o.why}
                </jh-notification>`)}
              ${e.svg?n`<div class="diagram" aria-hidden="true">${Pi(e.svg)}</div>`:p}
              ${t.spans.map(o=>this._span(o))}
              <div class="actions">
                <jh-button appearance="secondary" size="small" label="Evidence artifact"
                  href=${Cr(this.sha)} target="_blank"></jh-button>
                <span class="helper">One self-contained file: the same chain with its full records, verifiable offline — save it to hand an auditor complete evidence.</span>
              </div>`}})}
      </jh-card>`}};customElements.define("box-provenance",Wo);var Fo=class extends j{static styles=[C,$`
    :host { display: block; }
    form { display: grid; gap: var(--jh-space-400); max-width: 560px; }
    form .actions { padding-top: var(--jh-space-200); }
  `];static properties={_routes:{state:!0},_route:{state:!0},_key:{state:!0},_payload:{state:!0},_result:{state:!0},_error:{state:!0},_pending:{state:!0}};constructor(){super(),this._routes=null,this._route="",this._key="",this._payload="",this._result=null,this._error="",this._pending=!1}connectedCallback(){super.connectedCallback(),ze().then(e=>{this._routes=e.routes??[],!this._route&&this._routes.length&&(this._route=this._routes[0])}).catch(e=>{this._error=e.message,this._routes=[]})}async _submit(e){e.preventDefault(),this._pending=!0,this._error="",this._result=null;try{this._result=await Rr(this._route,this._key.trim(),this._payload),R(`${this._result.opened?"Opened":"Already open"}: ${E(this._payload)}`,"positive")}catch(t){this._error=t.message}finally{this._pending=!1}}render(){return n`
      <jh-card header-title="Submit work" header-subtitle="Open an ingress; the daemon's next tick takes it from there" padding="medium">
        <form @submit=${this._submit}>
          ${""}
          ${this._routes===null?Y("routes"):this._routes.length===0?n`<jh-notification appearance="neutral" type="alert" hide-dismiss-button>No routes are configured, so nothing can be submitted. Routes are operator configuration in box.yaml.</jh-notification>`:n`
                <jh-radio-group label="Route" name="route" .value=${this._route}
                  helper-text="Routes are the operator's configuration; work inherits the route's target and authority"
                  @jh-change=${e=>{this._route=e.target.value}}>
                  ${this._routes.map(e=>n`<jh-radio label=${e} value=${e} ?checked=${e===this._route}></jh-radio>`)}
                </jh-radio-group>`}
          <jh-input label="Occurrence key" required
            helper-text="The same key always names the same work — redelivery is safe"
            .value=${this._key}
            @jh-change=${e=>{this._key=e.target.value}}
            @input=${e=>{this._key=e.composedPath()[0].value??this._key}}></jh-input>
          <jh-input type="textarea" label="Task" rows="5" required
            helper-text="What should be done, in plain words. The first line is how the room will name it."
            .value=${this._payload}
            @jh-change=${e=>{this._payload=e.target.value}}
            @input=${e=>{this._payload=e.composedPath()[0].value??this._payload}}></jh-input>
          <div class="actions">
            <jh-button submit appearance="primary" label="Open ingress"
              ?pending=${this._pending} ?disabled=${!this._route||!this._key.trim()||!this._payload.trim()}></jh-button>
            ${!this._route||!this._key.trim()||!this._payload.trim()?n`<span class="helper">Needs a route, a key and a task.</span>`:p}
          </div>
        </form>
        ${this._error?n`<jh-notification appearance="negative" type="alert" hide-dismiss-button>${this._error}</jh-notification>`:p}
        ${this._result?n`
          <jh-notification appearance="positive" type="alert" hide-dismiss-button>
            ${this._result.opened?"Opened":"Already open"} — item
            <a href=${"/items/"+this._result.item}>${this._result.item}</a>
          </jh-notification>`:p}
      </jh-card>`}};customElements.define("box-submit",Fo);wr();var La=[{href:"/",label:"Dashboard"},{href:"/work",label:"Work"},{href:"/sessions",label:"Sessions"},{href:"/exceptions",label:"Exceptions"},{href:"/submit",label:"Submit"}],Yo=class extends j{static properties={_theme:{state:!0}};static styles=[C,$`
    .seg { display: inline-flex; border: 1px solid var(--jh-color-control-enabled);
           border-radius: var(--jh-border-radius-pill); overflow: hidden; }
    button { display: inline-flex; align-items: center; gap: var(--jh-space-100);
             font: var(--jh-font-helper-medium); cursor: pointer; padding: 4px var(--jh-space-300);
             border: 0; background: transparent; color: var(--jh-color-content-secondary-enabled); }
    button + button { border-left: 1px solid var(--jh-color-control-enabled); }
    button:hover { color: var(--jh-color-content-primary-enabled); background: var(--jh-color-container-secondary-hover); }
    button[aria-pressed="true"] { background: var(--jh-color-container-secondary-selected);
                                  color: var(--jh-color-content-primary-enabled); font-weight: 600; }
    button:focus-visible { border-radius: 0; position: relative; z-index: 1; }
    jh-icon { --jh-icon-color-fill: currentColor; }
    @media (max-width: 720px) { button span { display: none; } }
  `];constructor(){super(),this._theme=Te(),this._onTheme=e=>{this._theme=e.detail.theme}}connectedCallback(){super.connectedCallback(),window.addEventListener("box-theme",this._onTheme)}disconnectedCallback(){super.disconnectedCallback(),window.removeEventListener("box-theme",this._onTheme)}render(){return n`
      <div class="seg" role="group" aria-label="Theme">
        ${Nt.map(e=>n`
          <button type="button" aria-pressed=${this._theme===e.id?"true":"false"}
            title=${`${e.label} theme`} @click=${()=>Mr(e.id)}>
            ${Z(e.icon,"x-small")}<span>${e.label}</span>
          </button>`)}
      </div>`}};customElements.define("box-theme",Yo);var Go=class extends j{static properties={_status:{state:!0},_error:{state:!0}};static styles=[C,$`
    :host { display: block; }
    .strip { display: flex; align-items: center; gap: var(--jh-space-300); flex-wrap: wrap;
             padding: var(--jh-space-200) var(--jh-space-400);
             background: var(--jh-color-container-primary-enabled);
             border: 1px solid var(--jh-color-divider-secondary);
             border-radius: var(--jh-border-radius-300); }
    .state { display: flex; align-items: center; gap: var(--jh-space-300); flex: 1; min-width: 240px; flex-wrap: wrap; }
    .who { font: var(--jh-font-helper-regular); color: var(--jh-color-content-secondary-enabled); flex: 1 1 220px; }
    .who b { color: var(--jh-color-content-primary-enabled); font-weight: 600; }
    .id { display: flex; gap: var(--jh-space-300); flex-wrap: wrap; font: var(--jh-font-helper-regular);
          color: var(--jh-color-content-secondary-enabled); }
    .id b { color: var(--jh-color-content-primary-enabled); font-weight: 500; }
    .live { display: inline-flex; align-items: center; gap: var(--jh-space-100); font: var(--jh-font-helper-medium); }
    .live jh-icon { --jh-icon-color-fill: var(--jh-color-content-positive-enabled); }
    .live.down jh-icon { --jh-icon-color-fill: var(--jh-color-content-negative-enabled); }
  `];constructor(){super(),this._status=null,this._error="",this._activity=new N(this,{limit:1,onEvent:e=>{(e.scope==="control/claims"||e.kind==="config-reloaded")&&this._refresh()}})}connectedCallback(){super.connectedCallback(),this._refresh(),this._onDone=()=>this._refresh(),window.addEventListener("box-done",this._onDone)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer),window.removeEventListener("box-done",this._onDone)}_refresh(){clearTimeout(this._timer),this._timer=setTimeout(async()=>{try{this._status=await ze(),this._error=""}catch(e){this._error=e.message}},150)}render(){let e=this._status,t=e?.claims,o=this._activity.connected;return n`
      <div class="strip">
        <span class=${"live"+(o?"":" down")} role="status">
          ${Z("dot","x-small")}${o?"Live":"Reconnecting\u2026"}
        </span>
        <div class="state">
          ${e?n`
            <jh-tag size="small" class=${t?.stopped?"negative":"positive"}
              label=${t?.stopped?"Claims stopped":"Claims running"}></jh-tag>
            ${t?.actor?n`
              <span class="who">${t.stopped?"stopped":"resumed"} <box-when .at=${t.at}></box-when>
                by <b>${t.actor}</b>${t.reason?n` — ${t.reason}`:p}</span>`:n`<span class="who">nobody has pulled the cord</span>`}
            ${t?.stopped?n`<box-reason floating label="Resume claims" confirm="Resume" appearance="primary" glyph="play"
                  prompt="Resume claims" .action=${i=>Or(i).then(()=>R("Claims resumed","positive"))}></box-reason>`:n`<box-reason floating label="Stop claims" confirm="Stop" appearance="danger" glyph="stop"
                  prompt="Stop claims" .action=${i=>Pr(i).then(()=>R("Claims stopped \u2014 in-flight runs settle, nothing new starts","negative"))}></box-reason>`}`:n`<span class="who">${this._error||"Reading the factory\u2026"}</span>`}
        </div>
        ${e?n`
          <div class="id" aria-label="Daemon">
            <span>tenant <b>${e.tenant}</b></span>
            <span>box <b>${e.box}</b></span>
            <span>runner <b>${e.runner}</b></span>
            <span>version <b>${e.version}</b></span>
            <span>up <b>${k(e.uptime_sec)}</b></span>
          </div>`:p}
      </div>`}};customElements.define("box-claims",Go);var Qo=class extends j{static styles=[C,$`
    :host { display: block; max-width: 1080px; margin: 0 auto; padding: var(--jh-space-400); }
    .skip { position: absolute; left: -999px; top: var(--jh-space-200); padding: var(--jh-space-200) var(--jh-space-300);
            background: var(--jh-color-container-primary-enabled); border-radius: var(--jh-border-radius-200);
            box-shadow: var(--jh-shadow-300); z-index: 10; }
    .skip:focus { left: var(--jh-space-400); }
    header { display: flex; align-items: center; gap: var(--jh-space-400); flex-wrap: wrap; margin-bottom: var(--jh-space-300); }
    h1 { font: var(--jh-font-heading-medium-4); margin: 0; }
    nav { display: flex; gap: var(--jh-space-100); flex: 1; flex-wrap: wrap; }
    nav a { text-decoration: none; padding: 6px var(--jh-space-300); border-radius: var(--jh-border-radius-pill);
            color: var(--jh-color-content-primary-enabled); font: var(--jh-font-body-medium-1); }
    nav a:hover { background: var(--jh-color-container-secondary-hover); }
    nav a[aria-current="page"] { background: var(--jh-color-container-secondary-selected); font-weight: 600; }
    main { display: block; margin-top: var(--jh-space-400); }
    main:focus { outline: none; }
  `];_router=new Ce(this,[{path:"/",render:()=>n`<box-dashboard></box-dashboard>`},{path:"/work",render:()=>n`<box-work></box-work>`},{path:"/sessions",render:()=>n`<box-sessions></box-sessions>`},{path:"/exceptions",render:()=>n`<box-exceptions></box-exceptions>`},{path:"/items/:id",render:({id:e})=>n`<box-item .itemId=${e}></box-item>`},{path:"/provenance/:sha",render:({sha:e})=>n`<box-provenance .sha=${e}></box-provenance>`},{path:"/submit",render:()=>n`<box-submit></box-submit>`}],{fallback:{render:()=>n`
        <jh-card header-title="No such page" padding="medium">
          <p class="empty">Nothing lives at <code>${location.pathname}</code>. <a href="/">Back to the dashboard</a>.</p>
        </jh-card>`}});_current(e){return location.pathname===e?"page":p}render(){return n`
      <a class="skip" href="#main">Skip to content</a>
      <header>
        <h1>Box control room</h1>
        <nav aria-label="Primary">
          ${La.map(e=>n`<a href=${e.href} aria-current=${this._current(e.href)}>${e.label}</a>`)}
        </nav>
        <box-theme></box-theme>
      </header>
      <box-claims></box-claims>
      <main id="main" tabindex="-1">${this._router.outlet()}</main>
      <jh-toast-controller></jh-toast-controller>
    `}};customElements.define("box-app",Qo);export{Qo as BoxApp,Go as BoxClaims,Yo as BoxTheme};
/*! Bundled license information:

@lit/reactive-element/css-tag.js:
  (**
   * @license
   * Copyright 2019 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/reactive-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/lit-html.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-element/lit-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/is-server.js:
  (**
   * @license
   * Copyright 2022 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit-labs/router/routes.js:
  (**
   * @license
   * Copyright 2021 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit-labs/router/router.js:
  (**
   * @license
   * Copyright 2021 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/css-tag.js:
  (**
   * @license
   * Copyright 2019 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/reactive-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/lit-html.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-element/lit-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/directives/if-defined.js:
  (**
   * @license
   * Copyright 2018 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/css-tag.js:
  (**
   * @license
   * Copyright 2019 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/reactive-element/reactive-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/lit-html.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-element/lit-element.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/directive.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

lit-html/directives/unsafe-html.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)

@lit/task/task.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)
*/
