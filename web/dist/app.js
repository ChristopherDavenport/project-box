var At=globalThis,St=At.ShadowRoot&&(At.ShadyCSS===void 0||At.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,Kt=Symbol(),vo=new WeakMap,st=class{constructor(t,e,o){if(this._$cssResult$=!0,o!==Kt)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=t,this.t=e}get styleSheet(){let t=this.o,e=this.t;if(St&&t===void 0){let o=e!==void 0&&e.length===1;o&&(t=vo.get(e)),t===void 0&&((this.o=t=new CSSStyleSheet).replaceSync(this.cssText),o&&vo.set(e,t))}return t}toString(){return this.cssText}},go=i=>new st(typeof i=="string"?i:i+"",void 0,Kt),S=(i,...t)=>{let e=i.length===1?i[0]:t.reduce((o,r,s)=>o+(a=>{if(a._$cssResult$===!0)return a.cssText;if(typeof a=="number")return a;throw Error("Value passed to 'css' function must be a 'css' function result: "+a+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(r)+i[s+1],i[0]);return new st(e,i,Kt)},mo=(i,t)=>{if(St)i.adoptedStyleSheets=t.map(e=>e instanceof CSSStyleSheet?e:e.styleSheet);else for(let e of t){let o=document.createElement("style"),r=At.litNonce;r!==void 0&&o.setAttribute("nonce",r),o.textContent=e.cssText,i.appendChild(o)}},Ft=St?i=>i:i=>i instanceof CSSStyleSheet?(t=>{let e="";for(let o of t.cssRules)e+=o.cssText;return go(e)})(i):i;var{is:Or,defineProperty:Nr,getOwnPropertyDescriptor:Hr,getOwnPropertyNames:Dr,getOwnPropertySymbols:Mr,getPrototypeOf:Ir}=Object,Et=globalThis,fo=Et.trustedTypes,Br=fo?fo.emptyScript:"",qr=Et.reactiveElementPolyfillSupport,at=(i,t)=>i,Zt={toAttribute(i,t){switch(t){case Boolean:i=i?Br:null;break;case Object:case Array:i=i==null?i:JSON.stringify(i)}return i},fromAttribute(i,t){let e=i;switch(t){case Boolean:e=i!==null;break;case Number:e=i===null?null:Number(i);break;case Object:case Array:try{e=JSON.parse(i)}catch{e=null}}return e}},zt=(i,t)=>!Or(i,t),jo={attribute:!0,type:String,converter:Zt,reflect:!1,useDefault:!1,hasChanged:zt};Symbol.metadata??=Symbol("metadata"),Et.litPropertyMetadata??=new WeakMap;var C=class extends HTMLElement{static addInitializer(t){this._$Ei(),(this.l??=[]).push(t)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(t,e=jo){if(e.state&&(e.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(t)&&((e=Object.create(e)).wrapped=!0),this.elementProperties.set(t,e),!e.noAccessor){let o=Symbol(),r=this.getPropertyDescriptor(t,o,e);r!==void 0&&Nr(this.prototype,t,r)}}static getPropertyDescriptor(t,e,o){let{get:r,set:s}=Hr(this.prototype,t)??{get(){return this[e]},set(a){this[e]=a}};return{get:r,set(a){let h=r?.call(this);s?.call(this,a),this.requestUpdate(t,h,o)},configurable:!0,enumerable:!0}}static getPropertyOptions(t){return this.elementProperties.get(t)??jo}static _$Ei(){if(this.hasOwnProperty(at("elementProperties")))return;let t=Ir(this);t.finalize(),t.l!==void 0&&(this.l=[...t.l]),this.elementProperties=new Map(t.elementProperties)}static finalize(){if(this.hasOwnProperty(at("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(at("properties"))){let e=this.properties,o=[...Dr(e),...Mr(e)];for(let r of o)this.createProperty(r,e[r])}let t=this[Symbol.metadata];if(t!==null){let e=litPropertyMetadata.get(t);if(e!==void 0)for(let[o,r]of e)this.elementProperties.set(o,r)}this._$Eh=new Map;for(let[e,o]of this.elementProperties){let r=this._$Eu(e,o);r!==void 0&&this._$Eh.set(r,e)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(t){let e=[];if(Array.isArray(t)){let o=new Set(t.flat(1/0).reverse());for(let r of o)e.unshift(Ft(r))}else t!==void 0&&e.push(Ft(t));return e}static _$Eu(t,e){let o=e.attribute;return o===!1?void 0:typeof o=="string"?o:typeof t=="string"?t.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(t=>this.enableUpdating=t),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(t=>t(this))}addController(t){(this._$EO??=new Set).add(t),this.renderRoot!==void 0&&this.isConnected&&t.hostConnected?.()}removeController(t){this._$EO?.delete(t)}_$E_(){let t=new Map,e=this.constructor.elementProperties;for(let o of e.keys())this.hasOwnProperty(o)&&(t.set(o,this[o]),delete this[o]);t.size>0&&(this._$Ep=t)}createRenderRoot(){let t=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return mo(t,this.constructor.elementStyles),t}connectedCallback(){this.renderRoot??=this.createRenderRoot(),this.enableUpdating(!0),this._$EO?.forEach(t=>t.hostConnected?.())}enableUpdating(t){}disconnectedCallback(){this._$EO?.forEach(t=>t.hostDisconnected?.())}attributeChangedCallback(t,e,o){this._$AK(t,o)}_$ET(t,e){let o=this.constructor.elementProperties.get(t),r=this.constructor._$Eu(t,o);if(r!==void 0&&o.reflect===!0){let s=(o.converter?.toAttribute!==void 0?o.converter:Zt).toAttribute(e,o.type);this._$Em=t,s==null?this.removeAttribute(r):this.setAttribute(r,s),this._$Em=null}}_$AK(t,e){let o=this.constructor,r=o._$Eh.get(t);if(r!==void 0&&this._$Em!==r){let s=o.getPropertyOptions(r),a=typeof s.converter=="function"?{fromAttribute:s.converter}:s.converter?.fromAttribute!==void 0?s.converter:Zt;this._$Em=r;let h=a.fromAttribute(e,s.type);this[r]=h??this._$Ej?.get(r)??h,this._$Em=null}}requestUpdate(t,e,o,r=!1,s){if(t!==void 0){let a=this.constructor;if(r===!1&&(s=this[t]),o??=a.getPropertyOptions(t),!((o.hasChanged??zt)(s,e)||o.useDefault&&o.reflect&&s===this._$Ej?.get(t)&&!this.hasAttribute(a._$Eu(t,o))))return;this.C(t,e,o)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(t,e,{useDefault:o,reflect:r,wrapped:s},a){o&&!(this._$Ej??=new Map).has(t)&&(this._$Ej.set(t,a??e??this[t]),s!==!0||a!==void 0)||(this._$AL.has(t)||(this.hasUpdated||o||(e=void 0),this._$AL.set(t,e)),r===!0&&this._$Em!==t&&(this._$Eq??=new Set).add(t))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(e){Promise.reject(e)}let t=this.scheduleUpdate();return t!=null&&await t,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??=this.createRenderRoot(),this._$Ep){for(let[r,s]of this._$Ep)this[r]=s;this._$Ep=void 0}let o=this.constructor.elementProperties;if(o.size>0)for(let[r,s]of o){let{wrapped:a}=s,h=this[r];a!==!0||this._$AL.has(r)||h===void 0||this.C(r,void 0,s,h)}}let t=!1,e=this._$AL;try{t=this.shouldUpdate(e),t?(this.willUpdate(e),this._$EO?.forEach(o=>o.hostUpdate?.()),this.update(e)):this._$EM()}catch(o){throw t=!1,this._$EM(),o}t&&this._$AE(e)}willUpdate(t){}_$AE(t){this._$EO?.forEach(e=>e.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(t)),this.updated(t)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(t){return!0}update(t){this._$Eq&&=this._$Eq.forEach(e=>this._$ET(e,this[e])),this._$EM()}updated(t){}firstUpdated(t){}};C.elementStyles=[],C.shadowRootOptions={mode:"open"},C[at("elementProperties")]=new Map,C[at("finalized")]=new Map,qr?.({ReactiveElement:C}),(Et.reactiveElementVersions??=[]).push("2.1.2");var ee=globalThis,yo=i=>i,Ct=ee.trustedTypes,$o=Ct?Ct.createPolicy("lit-html",{createHTML:i=>i}):void 0,So="$lit$",P=`lit$${Math.random().toFixed(9).slice(2)}$`,Eo="?"+P,Vr=`<${Eo}>`,H=document,lt=()=>H.createComment(""),ht=i=>i===null||typeof i!="object"&&typeof i!="function",oe=Array.isArray,Wr=i=>oe(i)||typeof i?.[Symbol.iterator]=="function",Gt=`[ 	
\f\r]`,nt=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,xo=/-->/g,_o=/>/g,O=RegExp(`>|${Gt}(?:([^\\s"'>=/]+)(${Gt}*=${Gt}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),wo=/'/g,ko=/"/g,zo=/^(?:script|style|textarea|title)$/i,re=i=>(t,...e)=>({_$litType$:i,strings:t,values:e}),u=re(1),fi=re(2),ji=re(3),D=Symbol.for("lit-noChange"),f=Symbol.for("lit-nothing"),Ao=new WeakMap,N=H.createTreeWalker(H,129);function Co(i,t){if(!oe(i)||!i.hasOwnProperty("raw"))throw Error("invalid template strings array");return $o!==void 0?$o.createHTML(t):t}var Kr=(i,t)=>{let e=i.length-1,o=[],r,s=t===2?"<svg>":t===3?"<math>":"",a=nt;for(let h=0;h<e;h++){let n=i[h],l,p,d=-1,v=0;for(;v<n.length&&(a.lastIndex=v,p=a.exec(n),p!==null);)v=a.lastIndex,a===nt?p[1]==="!--"?a=xo:p[1]!==void 0?a=_o:p[2]!==void 0?(zo.test(p[2])&&(r=RegExp("</"+p[2],"g")),a=O):p[3]!==void 0&&(a=O):a===O?p[0]===">"?(a=r??nt,d=-1):p[1]===void 0?d=-2:(d=a.lastIndex-p[2].length,l=p[1],a=p[3]===void 0?O:p[3]==='"'?ko:wo):a===ko||a===wo?a=O:a===xo||a===_o?a=nt:(a=O,r=void 0);let g=a===O&&i[h+1].startsWith("/>")?" ":"";s+=a===nt?n+Vr:d>=0?(o.push(l),n.slice(0,d)+So+n.slice(d)+P+g):n+P+(d===-2?h:g)}return[Co(i,s+(i[e]||"<?>")+(t===2?"</svg>":t===3?"</math>":"")),o]},ct=class i{constructor({strings:t,_$litType$:e},o){let r;this.parts=[];let s=0,a=0,h=t.length-1,n=this.parts,[l,p]=Kr(t,e);if(this.el=i.createElement(l,o),N.currentNode=this.el.content,e===2||e===3){let d=this.el.content.firstChild;d.replaceWith(...d.childNodes)}for(;(r=N.nextNode())!==null&&n.length<h;){if(r.nodeType===1){if(r.hasAttributes())for(let d of r.getAttributeNames())if(d.endsWith(So)){let v=p[a++],g=r.getAttribute(d).split(P),k=/([.?@])?(.*)/.exec(v);n.push({type:1,index:s,name:k[2],strings:g,ctor:k[1]==="."?Qt:k[1]==="?"?Xt:k[1]==="@"?Jt:Y}),r.removeAttribute(d)}else d.startsWith(P)&&(n.push({type:6,index:s}),r.removeAttribute(d));if(zo.test(r.tagName)){let d=r.textContent.split(P),v=d.length-1;if(v>0){r.textContent=Ct?Ct.emptyScript:"";for(let g=0;g<v;g++)r.append(d[g],lt()),N.nextNode(),n.push({type:2,index:++s});r.append(d[v],lt())}}}else if(r.nodeType===8)if(r.data===Eo)n.push({type:2,index:s});else{let d=-1;for(;(d=r.data.indexOf(P,d+1))!==-1;)n.push({type:7,index:s}),d+=P.length-1}s++}}static createElement(t,e){let o=H.createElement("template");return o.innerHTML=t,o}};function G(i,t,e=i,o){if(t===D)return t;let r=o!==void 0?e._$Co?.[o]:e._$Cl,s=ht(t)?void 0:t._$litDirective$;return r?.constructor!==s&&(r?._$AO?.(!1),s===void 0?r=void 0:(r=new s(i),r._$AT(i,e,o)),o!==void 0?(e._$Co??=[])[o]=r:e._$Cl=r),r!==void 0&&(t=G(i,r._$AS(i,t.values),r,o)),t}var Yt=class{constructor(t,e){this._$AV=[],this._$AN=void 0,this._$AD=t,this._$AM=e}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(t){let{el:{content:e},parts:o}=this._$AD,r=(t?.creationScope??H).importNode(e,!0);N.currentNode=r;let s=N.nextNode(),a=0,h=0,n=o[0];for(;n!==void 0;){if(a===n.index){let l;n.type===2?l=new dt(s,s.nextSibling,this,t):n.type===1?l=new n.ctor(s,n.name,n.strings,this,t):n.type===6&&(l=new te(s,this,t)),this._$AV.push(l),n=o[++h]}a!==n?.index&&(s=N.nextNode(),a++)}return N.currentNode=H,r}p(t){let e=0;for(let o of this._$AV)o!==void 0&&(o.strings!==void 0?(o._$AI(t,o,e),e+=o.strings.length-2):o._$AI(t[e])),e++}},dt=class i{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(t,e,o,r){this.type=2,this._$AH=f,this._$AN=void 0,this._$AA=t,this._$AB=e,this._$AM=o,this.options=r,this._$Cv=r?.isConnected??!0}get parentNode(){let t=this._$AA.parentNode,e=this._$AM;return e!==void 0&&t?.nodeType===11&&(t=e.parentNode),t}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(t,e=this){t=G(this,t,e),ht(t)?t===f||t==null||t===""?(this._$AH!==f&&this._$AR(),this._$AH=f):t!==this._$AH&&t!==D&&this._(t):t._$litType$!==void 0?this.$(t):t.nodeType!==void 0?this.T(t):Wr(t)?this.k(t):this._(t)}O(t){return this._$AA.parentNode.insertBefore(t,this._$AB)}T(t){this._$AH!==t&&(this._$AR(),this._$AH=this.O(t))}_(t){this._$AH!==f&&ht(this._$AH)?this._$AA.nextSibling.data=t:this.T(H.createTextNode(t)),this._$AH=t}$(t){let{values:e,_$litType$:o}=t,r=typeof o=="number"?this._$AC(t):(o.el===void 0&&(o.el=ct.createElement(Co(o.h,o.h[0]),this.options)),o);if(this._$AH?._$AD===r)this._$AH.p(e);else{let s=new Yt(r,this),a=s.u(this.options);s.p(e),this.T(a),this._$AH=s}}_$AC(t){let e=Ao.get(t.strings);return e===void 0&&Ao.set(t.strings,e=new ct(t)),e}k(t){oe(this._$AH)||(this._$AH=[],this._$AR());let e=this._$AH,o,r=0;for(let s of t)r===e.length?e.push(o=new i(this.O(lt()),this.O(lt()),this,this.options)):o=e[r],o._$AI(s),r++;r<e.length&&(this._$AR(o&&o._$AB.nextSibling,r),e.length=r)}_$AR(t=this._$AA.nextSibling,e){for(this._$AP?.(!1,!0,e);t!==this._$AB;){let o=yo(t).nextSibling;yo(t).remove(),t=o}}setConnected(t){this._$AM===void 0&&(this._$Cv=t,this._$AP?.(t))}},Y=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(t,e,o,r,s){this.type=1,this._$AH=f,this._$AN=void 0,this.element=t,this.name=e,this._$AM=r,this.options=s,o.length>2||o[0]!==""||o[1]!==""?(this._$AH=Array(o.length-1).fill(new String),this.strings=o):this._$AH=f}_$AI(t,e=this,o,r){let s=this.strings,a=!1;if(s===void 0)t=G(this,t,e,0),a=!ht(t)||t!==this._$AH&&t!==D,a&&(this._$AH=t);else{let h=t,n,l;for(t=s[0],n=0;n<s.length-1;n++)l=G(this,h[o+n],e,n),l===D&&(l=this._$AH[n]),a||=!ht(l)||l!==this._$AH[n],l===f?t=f:t!==f&&(t+=(l??"")+s[n+1]),this._$AH[n]=l}a&&!r&&this.j(t)}j(t){t===f?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,t??"")}},Qt=class extends Y{constructor(){super(...arguments),this.type=3}j(t){this.element[this.name]=t===f?void 0:t}},Xt=class extends Y{constructor(){super(...arguments),this.type=4}j(t){this.element.toggleAttribute(this.name,!!t&&t!==f)}},Jt=class extends Y{constructor(t,e,o,r,s){super(t,e,o,r,s),this.type=5}_$AI(t,e=this){if((t=G(this,t,e,0)??f)===D)return;let o=this._$AH,r=t===f&&o!==f||t.capture!==o.capture||t.once!==o.once||t.passive!==o.passive,s=t!==f&&(o===f||r);r&&this.element.removeEventListener(this.name,this,o),s&&this.element.addEventListener(this.name,this,t),this._$AH=t}handleEvent(t){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,t):this._$AH.handleEvent(t)}},te=class{constructor(t,e,o){this.element=t,this.type=6,this._$AN=void 0,this._$AM=e,this.options=o}get _$AU(){return this._$AM._$AU}_$AI(t){G(this,t)}};var Fr=ee.litHtmlPolyfillSupport;Fr?.(ct,dt),(ee.litHtmlVersions??=[]).push("3.3.3");var To=(i,t,e)=>{let o=e?.renderBefore??t,r=o._$litPart$;if(r===void 0){let s=e?.renderBefore??null;o._$litPart$=r=new dt(t.insertBefore(lt(),s),s,void 0,e??{})}return r._$AI(i),r};var ie=globalThis,_=class extends C{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){let t=super.createRenderRoot();return this.renderOptions.renderBefore??=t.firstChild,t}update(t){let e=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(t),this._$Do=To(e,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return D}};_._$litElement$=!0,_.finalized=!0,ie.litElementHydrateSupport?.({LitElement:_});var Zr=ie.litElementPolyfillSupport;Zr?.({LitElement:_});(ie.litElementVersions??=[]).push("4.2.2");var Lo=new WeakMap,Po=i=>{if((e=>e.pattern!==void 0)(i))return i.pattern;let t=Lo.get(i);return t===void 0&&Lo.set(i,t=new URLPattern({pathname:i.path})),t},pt=class{constructor(t,e,o){this.routes=[],this.o=[],this.t={},this.i=r=>{if(r.routes===this)return;let s=r.routes;this.o.push(s),s.h=this,r.stopImmediatePropagation(),r.onDisconnect=()=>{this.o?.splice(this.o.indexOf(s)>>>0,1)};let a=Ro(this.t);a!==void 0&&s.goto(a)},(this.l=t).addController(this),this.routes=[...e],this.fallback=o?.fallback}link(t){if(t?.startsWith("/"))return t;if(t?.startsWith("."))throw Error("Not implemented");return t??=this.u,(this.h?.link()??"")+t}async goto(t){let e;if(this.routes.length===0&&this.fallback===void 0)e=t,this.u="",this.t={0:e};else{let o=this.p(t);if(o===void 0)throw Error("No route found for "+t);let r=Po(o).exec({pathname:t}),s=r?.pathname.groups??{};if(e=Ro(s),typeof o.enter=="function"&&await o.enter(s)===!1)return;this.v=o,this.t=s,this.u=e===void 0?t:t.substring(0,t.length-e.length)}if(e!==void 0)for(let o of this.o)o.goto(e);this.l.requestUpdate()}outlet(){return this.v?.render?.(this.t)}get params(){return this.t}p(t){let e=this.routes.find(o=>Po(o).test({pathname:t}));return e||this.fallback===void 0?e:this.fallback?{...this.fallback,path:"/*"}:void 0}hostConnected(){this.l.addEventListener(Q.eventName,this.i);let t=new Q(this);this.l.dispatchEvent(t),this._=t.onDisconnect}hostDisconnected(){this._?.(),this.h=void 0}},Ro=i=>{let t;for(let e of Object.keys(i))/\d+/.test(e)&&(t===void 0||e>t)&&(t=e);return t&&i[t]},Q=class i extends Event{constructor(t){super(i.eventName,{bubbles:!0,composed:!0,cancelable:!1}),this.routes=t}};Q.eventName="lit-routes-connected";var Gr=location.origin||location.protocol+"//"+location.host,ut=class extends pt{constructor(){super(...arguments),this.m=t=>{let e=t.button!==0||t.metaKey||t.ctrlKey||t.shiftKey;if(t.defaultPrevented||e)return;let o=t.composedPath().find(a=>a.tagName==="A");if(o===void 0||o.target!==""||o.hasAttribute("download")||o.getAttribute("rel")==="external")return;let r=o.href;if(r===""||r.startsWith("mailto:"))return;let s=window.location;o.origin===Gr&&(t.preventDefault(),r!==s.href&&(window.history.pushState({},"",r),this.goto(o.pathname)))},this.R=t=>{this.goto(window.location.pathname)}}hostConnected(){super.hostConnected(),window.addEventListener("click",this.m),window.addEventListener("popstate",this.R),this.goto(window.location.pathname)}hostDisconnected(){super.hostDisconnected(),window.removeEventListener("click",this.m),window.removeEventListener("popstate",this.R)}};function Uo(){let i=new URL(location.href),t=i.searchParams.get("token");t&&(document.cookie=`box_token=${encodeURIComponent(t)}; path=/; SameSite=Strict`,i.searchParams.delete("token"),history.replaceState({},"",i.toString()))}async function se(i,t,e){let o=await fetch(t,{method:i,headers:e?{"Content-Type":"application/json"}:void 0,body:e?JSON.stringify(e):void 0,credentials:"same-origin"});if(!o.ok){let r=`${i} ${t}: ${o.status}`;try{r=(await o.json()).error??r}catch{}throw new Error(r)}return o.json()}var ae=i=>M("/v1/status",i),ne=(i,t)=>M("/v1/items"+(i?.length?`?state=${encodeURIComponent(i.join(","))}`:""),t),Oo=i=>M("/v1/exceptions",i),No=(i,t)=>M(`/v1/items/${encodeURIComponent(i)}`,t),Ho=(i,t)=>M(`/v1/items/${encodeURIComponent(i)}/evidence`,t),Do=(i,t)=>M(`/v1/provenance/${encodeURIComponent(i)}`,t),Mo=i=>M("/v1/activity",i),Io=(i,t,e)=>se("POST","/v1/ingress",{route:i,key:t,payload:e}),Bo=i=>se("POST","/v1/stop",{reason:i}),qo=i=>se("POST","/v1/resume",{reason:i});async function M(i,t){let e=await fetch(i,{signal:t,credentials:"same-origin"});if(!e.ok)throw new Error(`${i}: ${e.status}`);return e.json()}var Tt=window,Lt=Tt.ShadowRoot&&(Tt.ShadyCSS===void 0||Tt.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,le=Symbol(),Vo=new WeakMap,bt=class{constructor(t,e,o){if(this._$cssResult$=!0,o!==le)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=t,this.t=e}get styleSheet(){let t=this.o,e=this.t;if(Lt&&t===void 0){let o=e!==void 0&&e.length===1;o&&(t=Vo.get(e)),t===void 0&&((this.o=t=new CSSStyleSheet).replaceSync(this.cssText),o&&Vo.set(e,t))}return t}toString(){return this.cssText}},Wo=i=>new bt(typeof i=="string"?i:i+"",void 0,le),j=(i,...t)=>{let e=i.length===1?i[0]:t.reduce((o,r,s)=>o+(a=>{if(a._$cssResult$===!0)return a.cssText;if(typeof a=="number")return a;throw Error("Value passed to 'css' function must be a 'css' function result: "+a+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(r)+i[s+1],i[0]);return new bt(e,i,le)},he=(i,t)=>{Lt?i.adoptedStyleSheets=t.map(e=>e instanceof CSSStyleSheet?e:e.styleSheet):t.forEach(e=>{let o=document.createElement("style"),r=Tt.litNonce;r!==void 0&&o.setAttribute("nonce",r),o.textContent=e.cssText,i.appendChild(o)})},Pt=Lt?i=>i:i=>i instanceof CSSStyleSheet?(t=>{let e="";for(let o of t.cssRules)e+=o.cssText;return Wo(e)})(i):i;var ce,Rt=window,Ko=Rt.trustedTypes,Yr=Ko?Ko.emptyScript:"",Fo=Rt.reactiveElementPolyfillSupport,pe={toAttribute(i,t){switch(t){case Boolean:i=i?Yr:null;break;case Object:case Array:i=i==null?i:JSON.stringify(i)}return i},fromAttribute(i,t){let e=i;switch(t){case Boolean:e=i!==null;break;case Number:e=i===null?null:Number(i);break;case Object:case Array:try{e=JSON.parse(i)}catch{e=null}}return e}},Zo=(i,t)=>t!==i&&(t==t||i==i),de={attribute:!0,type:String,converter:pe,reflect:!1,hasChanged:Zo},ue="finalized",T=class extends HTMLElement{constructor(){super(),this._$Ei=new Map,this.isUpdatePending=!1,this.hasUpdated=!1,this._$El=null,this._$Eu()}static addInitializer(t){var e;this.finalize(),((e=this.h)!==null&&e!==void 0?e:this.h=[]).push(t)}static get observedAttributes(){this.finalize();let t=[];return this.elementProperties.forEach((e,o)=>{let r=this._$Ep(o,e);r!==void 0&&(this._$Ev.set(r,o),t.push(r))}),t}static createProperty(t,e=de){if(e.state&&(e.attribute=!1),this.finalize(),this.elementProperties.set(t,e),!e.noAccessor&&!this.prototype.hasOwnProperty(t)){let o=typeof t=="symbol"?Symbol():"__"+t,r=this.getPropertyDescriptor(t,o,e);r!==void 0&&Object.defineProperty(this.prototype,t,r)}}static getPropertyDescriptor(t,e,o){return{get(){return this[e]},set(r){let s=this[t];this[e]=r,this.requestUpdate(t,s,o)},configurable:!0,enumerable:!0}}static getPropertyOptions(t){return this.elementProperties.get(t)||de}static finalize(){if(this.hasOwnProperty(ue))return!1;this[ue]=!0;let t=Object.getPrototypeOf(this);if(t.finalize(),t.h!==void 0&&(this.h=[...t.h]),this.elementProperties=new Map(t.elementProperties),this._$Ev=new Map,this.hasOwnProperty("properties")){let e=this.properties,o=[...Object.getOwnPropertyNames(e),...Object.getOwnPropertySymbols(e)];for(let r of o)this.createProperty(r,e[r])}return this.elementStyles=this.finalizeStyles(this.styles),!0}static finalizeStyles(t){let e=[];if(Array.isArray(t)){let o=new Set(t.flat(1/0).reverse());for(let r of o)e.unshift(Pt(r))}else t!==void 0&&e.push(Pt(t));return e}static _$Ep(t,e){let o=e.attribute;return o===!1?void 0:typeof o=="string"?o:typeof t=="string"?t.toLowerCase():void 0}_$Eu(){var t;this._$E_=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$Eg(),this.requestUpdate(),(t=this.constructor.h)===null||t===void 0||t.forEach(e=>e(this))}addController(t){var e,o;((e=this._$ES)!==null&&e!==void 0?e:this._$ES=[]).push(t),this.renderRoot!==void 0&&this.isConnected&&((o=t.hostConnected)===null||o===void 0||o.call(t))}removeController(t){var e;(e=this._$ES)===null||e===void 0||e.splice(this._$ES.indexOf(t)>>>0,1)}_$Eg(){this.constructor.elementProperties.forEach((t,e)=>{this.hasOwnProperty(e)&&(this._$Ei.set(e,this[e]),delete this[e])})}createRenderRoot(){var t;let e=(t=this.shadowRoot)!==null&&t!==void 0?t:this.attachShadow(this.constructor.shadowRootOptions);return he(e,this.constructor.elementStyles),e}connectedCallback(){var t;this.renderRoot===void 0&&(this.renderRoot=this.createRenderRoot()),this.enableUpdating(!0),(t=this._$ES)===null||t===void 0||t.forEach(e=>{var o;return(o=e.hostConnected)===null||o===void 0?void 0:o.call(e)})}enableUpdating(t){}disconnectedCallback(){var t;(t=this._$ES)===null||t===void 0||t.forEach(e=>{var o;return(o=e.hostDisconnected)===null||o===void 0?void 0:o.call(e)})}attributeChangedCallback(t,e,o){this._$AK(t,o)}_$EO(t,e,o=de){var r;let s=this.constructor._$Ep(t,o);if(s!==void 0&&o.reflect===!0){let a=(((r=o.converter)===null||r===void 0?void 0:r.toAttribute)!==void 0?o.converter:pe).toAttribute(e,o.type);this._$El=t,a==null?this.removeAttribute(s):this.setAttribute(s,a),this._$El=null}}_$AK(t,e){var o;let r=this.constructor,s=r._$Ev.get(t);if(s!==void 0&&this._$El!==s){let a=r.getPropertyOptions(s),h=typeof a.converter=="function"?{fromAttribute:a.converter}:((o=a.converter)===null||o===void 0?void 0:o.fromAttribute)!==void 0?a.converter:pe;this._$El=s,this[s]=h.fromAttribute(e,a.type),this._$El=null}}requestUpdate(t,e,o){let r=!0;t!==void 0&&(((o=o||this.constructor.getPropertyOptions(t)).hasChanged||Zo)(this[t],e)?(this._$AL.has(t)||this._$AL.set(t,e),o.reflect===!0&&this._$El!==t&&(this._$EC===void 0&&(this._$EC=new Map),this._$EC.set(t,o))):r=!1),!this.isUpdatePending&&r&&(this._$E_=this._$Ej())}async _$Ej(){this.isUpdatePending=!0;try{await this._$E_}catch(e){Promise.reject(e)}let t=this.scheduleUpdate();return t!=null&&await t,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){var t;if(!this.isUpdatePending)return;this.hasUpdated,this._$Ei&&(this._$Ei.forEach((r,s)=>this[s]=r),this._$Ei=void 0);let e=!1,o=this._$AL;try{e=this.shouldUpdate(o),e?(this.willUpdate(o),(t=this._$ES)===null||t===void 0||t.forEach(r=>{var s;return(s=r.hostUpdate)===null||s===void 0?void 0:s.call(r)}),this.update(o)):this._$Ek()}catch(r){throw e=!1,this._$Ek(),r}e&&this._$AE(o)}willUpdate(t){}_$AE(t){var e;(e=this._$ES)===null||e===void 0||e.forEach(o=>{var r;return(r=o.hostUpdated)===null||r===void 0?void 0:r.call(o)}),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(t)),this.updated(t)}_$Ek(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$E_}shouldUpdate(t){return!0}update(t){this._$EC!==void 0&&(this._$EC.forEach((e,o)=>this._$EO(o,this[o],e)),this._$EC=void 0),this._$Ek()}updated(t){}firstUpdated(t){}};T[ue]=!0,T.elementProperties=new Map,T.elementStyles=[],T.shadowRootOptions={mode:"open"},Fo?.({ReactiveElement:T}),((ce=Rt.reactiveElementVersions)!==null&&ce!==void 0?ce:Rt.reactiveElementVersions=[]).push("1.6.3");var be,Ut=window,X=Ut.trustedTypes,Go=X?X.createPolicy("lit-html",{createHTML:i=>i}):void 0,ge="$lit$",R=`lit$${(Math.random()+"").slice(9)}$`,or="?"+R,Qr=`<${or}>`,q=document,gt=()=>q.createComment(""),mt=i=>i===null||typeof i!="object"&&typeof i!="function",rr=Array.isArray,Xr=i=>rr(i)||typeof i?.[Symbol.iterator]=="function",ve=`[ 	
\f\r]`,vt=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,Yo=/-->/g,Qo=/>/g,I=RegExp(`>|${ve}(?:([^\\s"'>=/]+)(${ve}*=${ve}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),Xo=/'/g,Jo=/"/g,ir=/^(?:script|style|textarea|title)$/i,sr=i=>(t,...e)=>({_$litType$:i,strings:t,values:e}),c=sr(1),qi=sr(2),V=Symbol.for("lit-noChange"),y=Symbol.for("lit-nothing"),tr=new WeakMap,B=q.createTreeWalker(q,129,null,!1);function ar(i,t){if(!Array.isArray(i)||!i.hasOwnProperty("raw"))throw Error("invalid template strings array");return Go!==void 0?Go.createHTML(t):t}var Jr=(i,t)=>{let e=i.length-1,o=[],r,s=t===2?"<svg>":"",a=vt;for(let h=0;h<e;h++){let n=i[h],l,p,d=-1,v=0;for(;v<n.length&&(a.lastIndex=v,p=a.exec(n),p!==null);)v=a.lastIndex,a===vt?p[1]==="!--"?a=Yo:p[1]!==void 0?a=Qo:p[2]!==void 0?(ir.test(p[2])&&(r=RegExp("</"+p[2],"g")),a=I):p[3]!==void 0&&(a=I):a===I?p[0]===">"?(a=r??vt,d=-1):p[1]===void 0?d=-2:(d=a.lastIndex-p[2].length,l=p[1],a=p[3]===void 0?I:p[3]==='"'?Jo:Xo):a===Jo||a===Xo?a=I:a===Yo||a===Qo?a=vt:(a=I,r=void 0);let g=a===I&&i[h+1].startsWith("/>")?" ":"";s+=a===vt?n+Qr:d>=0?(o.push(l),n.slice(0,d)+ge+n.slice(d)+R+g):n+R+(d===-2?(o.push(void 0),h):g)}return[ar(i,s+(i[e]||"<?>")+(t===2?"</svg>":"")),o]},ft=class i{constructor({strings:t,_$litType$:e},o){let r;this.parts=[];let s=0,a=0,h=t.length-1,n=this.parts,[l,p]=Jr(t,e);if(this.el=i.createElement(l,o),B.currentNode=this.el.content,e===2){let d=this.el.content,v=d.firstChild;v.remove(),d.append(...v.childNodes)}for(;(r=B.nextNode())!==null&&n.length<h;){if(r.nodeType===1){if(r.hasAttributes()){let d=[];for(let v of r.getAttributeNames())if(v.endsWith(ge)||v.startsWith(R)){let g=p[a++];if(d.push(v),g!==void 0){let k=r.getAttribute(g.toLowerCase()+ge).split(R),w=/([.?@])?(.*)/.exec(g);n.push({type:1,index:s,name:w[2],strings:k,ctor:w[1]==="."?fe:w[1]==="?"?je:w[1]==="@"?ye:tt})}else n.push({type:6,index:s})}for(let v of d)r.removeAttribute(v)}if(ir.test(r.tagName)){let d=r.textContent.split(R),v=d.length-1;if(v>0){r.textContent=X?X.emptyScript:"";for(let g=0;g<v;g++)r.append(d[g],gt()),B.nextNode(),n.push({type:2,index:++s});r.append(d[v],gt())}}}else if(r.nodeType===8)if(r.data===or)n.push({type:2,index:s});else{let d=-1;for(;(d=r.data.indexOf(R,d+1))!==-1;)n.push({type:7,index:s}),d+=R.length-1}s++}}static createElement(t,e){let o=q.createElement("template");return o.innerHTML=t,o}};function J(i,t,e=i,o){var r,s,a,h;if(t===V)return t;let n=o!==void 0?(r=e._$Co)===null||r===void 0?void 0:r[o]:e._$Cl,l=mt(t)?void 0:t._$litDirective$;return n?.constructor!==l&&((s=n?._$AO)===null||s===void 0||s.call(n,!1),l===void 0?n=void 0:(n=new l(i),n._$AT(i,e,o)),o!==void 0?((a=(h=e)._$Co)!==null&&a!==void 0?a:h._$Co=[])[o]=n:e._$Cl=n),n!==void 0&&(t=J(i,n._$AS(i,t.values),n,o)),t}var me=class{constructor(t,e){this._$AV=[],this._$AN=void 0,this._$AD=t,this._$AM=e}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(t){var e;let{el:{content:o},parts:r}=this._$AD,s=((e=t?.creationScope)!==null&&e!==void 0?e:q).importNode(o,!0);B.currentNode=s;let a=B.nextNode(),h=0,n=0,l=r[0];for(;l!==void 0;){if(h===l.index){let p;l.type===2?p=new jt(a,a.nextSibling,this,t):l.type===1?p=new l.ctor(a,l.name,l.strings,this,t):l.type===6&&(p=new $e(a,this,t)),this._$AV.push(p),l=r[++n]}h!==l?.index&&(a=B.nextNode(),h++)}return B.currentNode=q,s}v(t){let e=0;for(let o of this._$AV)o!==void 0&&(o.strings!==void 0?(o._$AI(t,o,e),e+=o.strings.length-2):o._$AI(t[e])),e++}},jt=class i{constructor(t,e,o,r){var s;this.type=2,this._$AH=y,this._$AN=void 0,this._$AA=t,this._$AB=e,this._$AM=o,this.options=r,this._$Cp=(s=r?.isConnected)===null||s===void 0||s}get _$AU(){var t,e;return(e=(t=this._$AM)===null||t===void 0?void 0:t._$AU)!==null&&e!==void 0?e:this._$Cp}get parentNode(){let t=this._$AA.parentNode,e=this._$AM;return e!==void 0&&t?.nodeType===11&&(t=e.parentNode),t}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(t,e=this){t=J(this,t,e),mt(t)?t===y||t==null||t===""?(this._$AH!==y&&this._$AR(),this._$AH=y):t!==this._$AH&&t!==V&&this._(t):t._$litType$!==void 0?this.g(t):t.nodeType!==void 0?this.$(t):Xr(t)?this.T(t):this._(t)}k(t){return this._$AA.parentNode.insertBefore(t,this._$AB)}$(t){this._$AH!==t&&(this._$AR(),this._$AH=this.k(t))}_(t){this._$AH!==y&&mt(this._$AH)?this._$AA.nextSibling.data=t:this.$(q.createTextNode(t)),this._$AH=t}g(t){var e;let{values:o,_$litType$:r}=t,s=typeof r=="number"?this._$AC(t):(r.el===void 0&&(r.el=ft.createElement(ar(r.h,r.h[0]),this.options)),r);if(((e=this._$AH)===null||e===void 0?void 0:e._$AD)===s)this._$AH.v(o);else{let a=new me(s,this),h=a.u(this.options);a.v(o),this.$(h),this._$AH=a}}_$AC(t){let e=tr.get(t.strings);return e===void 0&&tr.set(t.strings,e=new ft(t)),e}T(t){rr(this._$AH)||(this._$AH=[],this._$AR());let e=this._$AH,o,r=0;for(let s of t)r===e.length?e.push(o=new i(this.k(gt()),this.k(gt()),this,this.options)):o=e[r],o._$AI(s),r++;r<e.length&&(this._$AR(o&&o._$AB.nextSibling,r),e.length=r)}_$AR(t=this._$AA.nextSibling,e){var o;for((o=this._$AP)===null||o===void 0||o.call(this,!1,!0,e);t&&t!==this._$AB;){let r=t.nextSibling;t.remove(),t=r}}setConnected(t){var e;this._$AM===void 0&&(this._$Cp=t,(e=this._$AP)===null||e===void 0||e.call(this,t))}},tt=class{constructor(t,e,o,r,s){this.type=1,this._$AH=y,this._$AN=void 0,this.element=t,this.name=e,this._$AM=r,this.options=s,o.length>2||o[0]!==""||o[1]!==""?(this._$AH=Array(o.length-1).fill(new String),this.strings=o):this._$AH=y}get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}_$AI(t,e=this,o,r){let s=this.strings,a=!1;if(s===void 0)t=J(this,t,e,0),a=!mt(t)||t!==this._$AH&&t!==V,a&&(this._$AH=t);else{let h=t,n,l;for(t=s[0],n=0;n<s.length-1;n++)l=J(this,h[o+n],e,n),l===V&&(l=this._$AH[n]),a||(a=!mt(l)||l!==this._$AH[n]),l===y?t=y:t!==y&&(t+=(l??"")+s[n+1]),this._$AH[n]=l}a&&!r&&this.j(t)}j(t){t===y?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,t??"")}},fe=class extends tt{constructor(){super(...arguments),this.type=3}j(t){this.element[this.name]=t===y?void 0:t}},ti=X?X.emptyScript:"",je=class extends tt{constructor(){super(...arguments),this.type=4}j(t){t&&t!==y?this.element.setAttribute(this.name,ti):this.element.removeAttribute(this.name)}},ye=class extends tt{constructor(t,e,o,r,s){super(t,e,o,r,s),this.type=5}_$AI(t,e=this){var o;if((t=(o=J(this,t,e,0))!==null&&o!==void 0?o:y)===V)return;let r=this._$AH,s=t===y&&r!==y||t.capture!==r.capture||t.once!==r.once||t.passive!==r.passive,a=t!==y&&(r===y||s);s&&this.element.removeEventListener(this.name,this,r),a&&this.element.addEventListener(this.name,this,t),this._$AH=t}handleEvent(t){var e,o;typeof this._$AH=="function"?this._$AH.call((o=(e=this.options)===null||e===void 0?void 0:e.host)!==null&&o!==void 0?o:this.element,t):this._$AH.handleEvent(t)}},$e=class{constructor(t,e,o){this.element=t,this.type=6,this._$AN=void 0,this._$AM=e,this.options=o}get _$AU(){return this._$AM._$AU}_$AI(t){J(this,t)}};var er=Ut.litHtmlPolyfillSupport;er?.(ft,jt),((be=Ut.litHtmlVersions)!==null&&be!==void 0?be:Ut.litHtmlVersions=[]).push("2.8.0");var nr=(i,t,e)=>{var o,r;let s=(o=e?.renderBefore)!==null&&o!==void 0?o:t,a=s._$litPart$;if(a===void 0){let h=(r=e?.renderBefore)!==null&&r!==void 0?r:null;s._$litPart$=a=new jt(t.insertBefore(gt(),h),h,void 0,e??{})}return a._$AI(i),a};var xe,_e;var m=class extends T{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){var t,e;let o=super.createRenderRoot();return(t=(e=this.renderOptions).renderBefore)!==null&&t!==void 0||(e.renderBefore=o.firstChild),o}update(t){let e=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(t),this._$Do=nr(e,this.renderRoot,this.renderOptions)}connectedCallback(){var t;super.connectedCallback(),(t=this._$Do)===null||t===void 0||t.setConnected(!0)}disconnectedCallback(){var t;super.disconnectedCallback(),(t=this._$Do)===null||t===void 0||t.setConnected(!1)}render(){return V}};m.finalized=!0,m._$litElement$=!0,(xe=globalThis.litElementHydrateSupport)===null||xe===void 0||xe.call(globalThis,{LitElement:m});var lr=globalThis.litElementPolyfillSupport;lr?.({LitElement:m});((_e=globalThis.litElementVersions)!==null&&_e!==void 0?_e:globalThis.litElementVersions=[]).push("3.3.3");var b=i=>i??y;var we=class extends m{#t;#e;static get styles(){return j`
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
    `}static get properties(){return{accessibleLabel:{type:String,attribute:"accessible-label"},accessibleValueText:{type:String,attribute:"accessible-valuetext"},hideValue:{type:Boolean,attribute:"hide-value"},indeterminate:{type:Boolean,reflect:!0},label:{type:String,reflect:!0},max:{type:Number},min:{type:Number},size:{type:String,reflect:!0},type:{type:String,reflect:!0},value:{type:Number}}}constructor(){super(),this.#t=this.attachInternals(),this.indeterminate=!1,this.label=null,this.size="medium",this.type="linear",this.hideValue=!1,this.#t.ariaLabel,this.#t.ariaValueMax,this.#t.ariaValueMin,this.#t.ariaValueNow,this.#t.ariaValueText,this.#t.role="progressbar",this.accessibleLabel=null,this.max=100,this.min=0,this.value=0,this.accessibleValueText=null}connectedCallback(){super.connectedCallback(),new MutationObserver(this.#o.bind(this)).observe(this,{attributeFilter:["indeterminate"]})}#o(){this.indeterminate&&(this.value=null)}#r(t,e){let o=this.#t[t];e!==o&&(this.#t[t]=e),this.requestUpdate(t,o)}get label(){return this.#e}set label(t){let e=this.label;t!==e&&(this.#e=t,this.accessibleLabel===null&&(this.accessibleLabel=t)),this.requestUpdate("label",e)}get accessibleLabel(){return this.#t.ariaLabel}set accessibleLabel(t){this.#r("ariaLabel",t)}get min(){return this.#t.ariaValueMin}set min(t){this.#r("ariaValueMin",t)}get max(){return this.#t.ariaValueMax}set max(t){this.#r("ariaValueMax",t)}get accessibleValueText(){return this.#t.ariaValueText}set accessibleValueText(t){this.#r("ariaValueText",t)}get value(){return this.#t.ariaValueNow}set value(t){this.#r("ariaValueNow",t)}#i(t,e,o){let r;e?r=75:o===null?r=0:r=o;let s={small:[6,6,37.8],medium:[8,8,50.3],large:[16,16,100.8]},[a,h,n]=s[t],l=n-n*Number(r)/100;return c`
      <svg>
        <circle
          class="circular-progress-bar"
          r=${a}
          cx=${h}
          cy=${h}
          c=${n}
        ></circle>
        <circle
          class="circular-progress-bar-value"
          r=${a}
          cx=${h}
          cy=${h}
          c=${n}
          style="stroke-dashoffset:${l}px"
        ></circle>
      </svg>
    `}#s(t){return c`
      <div class="linear-progress-bar">
        <div
          class="linear-progress-bar-value"
          style=${b(this.indeterminate?null:`width:${t||0}%`)}
        ></div>
      </div>
    `}render(){let t,e,o,r,s=Math.round((+this.value-+this.min)/(+this.max-+this.min)*100);return this.label&&(r=c`<label>${this.label}</label>`),this.type==="linear"?t=this.#s(s):this.type==="circular"&&(t=this.#i(this.size,this.indeterminate,s)),this.value&&!this.hideValue&&!this.indeterminate&&(this.label&&this.type==="linear"&&(o="has-label"),e=c`
        <span class=${b(o)}>${s+"%"}</span>
      `),c`
      <div class="container">
        ${this.label||this.value&&!this.hideValue?c`<div class="text-content">${r} ${e}</div>`:null}
        ${t}
      </div>
    `}};customElements.define("jh-progress",we);var ke=class extends m{static get formAssociated(){return!0}#t;#e;static get styles(){return j`
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
    `}static get properties(){return{accessibleDisabled:{type:String,attribute:"accessible-disabled",reflect:!0},accessibleLabel:{type:String,attribute:"accessible-label"},appearance:{type:String,reflect:!0},block:{type:Boolean},disabled:{type:Boolean,reflect:!0},href:{type:String},iconPosition:{type:String,attribute:"icon-position",reflect:!0},pending:{type:Boolean,reflect:!0},label:{type:String},name:{type:String},size:{type:String,reflect:!0},submit:{type:Boolean},target:{type:String},value:{type:String}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.form,this.accessibleDisabled=null,this.accessibleLabel=null,this.appearance="secondary",this.block=!1,this.disabled=!1,this.href=null,this.iconPosition="before",this.pending=!1,this.label=null,this.name=null,this.size="medium",this.submit=!1,this.target=null,this.value=null,this.addEventListener("click",this.#r),this.addEventListener("keydown",this.#i)}get value(){return this.#e}set value(t){let e=this.#e;t!==e&&(this.#e=t,this.#t.setFormValue(t)),this.requestUpdate("value",e)}firstUpdated(){this.#o(),new ResizeObserver(this.#o.bind(this)).observe(this)}#o(){let{width:t}=this.getBoundingClientRect();this.style.setProperty("--populated-button-width",`${t}px`)}formDisabledCallback(t){this.disabled=t}#r(t){this.submit&&this.#t.form&&!this.disabled&&this.#t.form.requestSubmit()}#i(t){(t.code==="Space"||t.code==="Enter")&&(this.accessibleDisabled==="true"||this.pending)&&t.preventDefault()}#s(){this.firstElementChild.setAttribute("aria-hidden","true"),this.firstElementChild.setAttribute("size","medium")}#a(t,e){let o,r;return e&&(r=c`<span>${e}</span>`),t&&!this.disabled?o=c`
        <jh-progress type="circular" indeterminate></jh-progress>
      `:o=c`
        <slot
          name="jh-button-icon"
          @slotchange=${this.#s}
        ></slot>
        ${r}
      `,c` <span class="content-wrapper">${o}</span> `}render(){let t=this.#a(this.pending,this.label),e;return this.accessibleDisabled!=="false"&&(e=this.accessibleDisabled),this.href?c`
        <a
          tabindex="0"
          aria-disabled=${b(e)}
          aria-label=${b(this.accessibleLabel)}
          ?disabled=${this.disabled}
          href=${b(this.disabled?null:this.href)}
          target=${b(this.disabled?null:this.target)}
        >
          ${t}
        </a>
      `:c`
        <button
          tabindex="0"
          aria-disabled=${b(e)}
          aria-label=${b(this.accessibleLabel)}
          ?disabled=${this.disabled}
          type=${this.submit?"submit":"button"}
        >
          ${t}
        </button>
      `}};customElements.define("jh-button",ke);var Ae=class extends m{static get styles(){return j`
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
      `}static get properties(){return{inset:{type:Number,reflect:!0}}}constructor(){super(),this.inset=null}};customElements.define("jh-divider",Ae);var Se=class extends m{static get styles(){return j`
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
    `}static get properties(){return{footerDividerInset:{type:Number,attribute:"footer-divider-inset"},headerDividerInset:{type:Number,attribute:"header-divider-inset"},showFooterDivider:{type:Boolean,attribute:"show-footer-divider",reflect:!0},showHeaderDivider:{type:Boolean,attribute:"show-header-divider",reflect:!0},padding:{type:String,reflect:!0},headerSubtitle:{type:String,attribute:"header-subtitle"},headerTitle:{type:String,attribute:"header-title"},titleHeadingLevel:{type:Number,attribute:"title-heading-level"}}}constructor(){super(),this.footerDividerInset=null,this.headerDividerInset=null,this.titleHeadingLevel=2,this.padding="medium",this.showFooterDivider=!1,this.showHeaderDivider=!1,this.headerSubtitle=null,this.headerTitle=null}#t(){switch(this.titleHeadingLevel){case 1:return c`<h1>${this.headerTitle}</h1>`;case 2:return c`<h2>${this.headerTitle}</h2>`;case 3:return c`<h3>${this.headerTitle}</h3>`;case 4:return c`<h4>${this.headerTitle}</h4>`;case 5:return c`<h5>${this.headerTitle}</h5>`;case 6:return c`<h6>${this.headerTitle}</h6>`;default:return c`<h2>${this.headerTitle}</h2>`}}#e(t){t.target.classList.add("display-slot")}render(){let t,e,o;return(this.headerTitle||this.headerSubtitle)&&(t=c`
        <div class="display-header">
          ${this.headerTitle?this.#t():null}
          ${this.headerSubtitle?c`<p>${this.headerSubtitle}</p>`:null}
        </div>
      `),this.showHeaderDivider&&(e=c`
        <jh-divider
          class="card-header-divider"
          inset=${b(this.headerDividerInset?this.headerDividerInset:null)}
        >
        </jh-divider>
      `),this.showFooterDivider&&(o=c`
        <jh-divider
          class="card-footer-divider"
          inset=${b(this.footerDividerInset?this.footerDividerInset:null)}
        >
        </jh-divider>
      `),c`
      <slot name="jh-card-media" @slotchange=${this.#e}></slot>
      <slot name="jh-card-header" @slotchange=${this.#e}>${t}</slot>
      ${e}
      <slot @slotchange=${this.#e}></slot>
      ${o}
      <slot name="jh-card-footer" @slotchange=${this.#e}></slot>
    `}};customElements.define("jh-card",Se);var Ot=window,Nt=Ot.ShadowRoot&&(Ot.ShadyCSS===void 0||Ot.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,Ee=Symbol(),hr=new WeakMap,yt=class{constructor(t,e,o){if(this._$cssResult$=!0,o!==Ee)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=t,this.t=e}get styleSheet(){let t=this.o,e=this.t;if(Nt&&t===void 0){let o=e!==void 0&&e.length===1;o&&(t=hr.get(e)),t===void 0&&((this.o=t=new CSSStyleSheet).replaceSync(this.cssText),o&&hr.set(e,t))}return t}toString(){return this.cssText}},cr=i=>new yt(typeof i=="string"?i:i+"",void 0,Ee),A=(i,...t)=>{let e=i.length===1?i[0]:t.reduce((o,r,s)=>o+(a=>{if(a._$cssResult$===!0)return a.cssText;if(typeof a=="number")return a;throw Error("Value passed to 'css' function must be a 'css' function result: "+a+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(r)+i[s+1],i[0]);return new yt(e,i,Ee)},ze=(i,t)=>{Nt?i.adoptedStyleSheets=t.map(e=>e instanceof CSSStyleSheet?e:e.styleSheet):t.forEach(e=>{let o=document.createElement("style"),r=Ot.litNonce;r!==void 0&&o.setAttribute("nonce",r),o.textContent=e.cssText,i.appendChild(o)})},Ht=Nt?i=>i:i=>i instanceof CSSStyleSheet?(t=>{let e="";for(let o of t.cssRules)e+=o.cssText;return cr(e)})(i):i;var Ce,Dt=window,dr=Dt.trustedTypes,ei=dr?dr.emptyScript:"",pr=Dt.reactiveElementPolyfillSupport,Le={toAttribute(i,t){switch(t){case Boolean:i=i?ei:null;break;case Object:case Array:i=i==null?i:JSON.stringify(i)}return i},fromAttribute(i,t){let e=i;switch(t){case Boolean:e=i!==null;break;case Number:e=i===null?null:Number(i);break;case Object:case Array:try{e=JSON.parse(i)}catch{e=null}}return e}},ur=(i,t)=>t!==i&&(t==t||i==i),Te={attribute:!0,type:String,converter:Le,reflect:!1,hasChanged:ur},Pe="finalized",L=class extends HTMLElement{constructor(){super(),this._$Ei=new Map,this.isUpdatePending=!1,this.hasUpdated=!1,this._$El=null,this._$Eu()}static addInitializer(t){var e;this.finalize(),((e=this.h)!==null&&e!==void 0?e:this.h=[]).push(t)}static get observedAttributes(){this.finalize();let t=[];return this.elementProperties.forEach((e,o)=>{let r=this._$Ep(o,e);r!==void 0&&(this._$Ev.set(r,o),t.push(r))}),t}static createProperty(t,e=Te){if(e.state&&(e.attribute=!1),this.finalize(),this.elementProperties.set(t,e),!e.noAccessor&&!this.prototype.hasOwnProperty(t)){let o=typeof t=="symbol"?Symbol():"__"+t,r=this.getPropertyDescriptor(t,o,e);r!==void 0&&Object.defineProperty(this.prototype,t,r)}}static getPropertyDescriptor(t,e,o){return{get(){return this[e]},set(r){let s=this[t];this[e]=r,this.requestUpdate(t,s,o)},configurable:!0,enumerable:!0}}static getPropertyOptions(t){return this.elementProperties.get(t)||Te}static finalize(){if(this.hasOwnProperty(Pe))return!1;this[Pe]=!0;let t=Object.getPrototypeOf(this);if(t.finalize(),t.h!==void 0&&(this.h=[...t.h]),this.elementProperties=new Map(t.elementProperties),this._$Ev=new Map,this.hasOwnProperty("properties")){let e=this.properties,o=[...Object.getOwnPropertyNames(e),...Object.getOwnPropertySymbols(e)];for(let r of o)this.createProperty(r,e[r])}return this.elementStyles=this.finalizeStyles(this.styles),!0}static finalizeStyles(t){let e=[];if(Array.isArray(t)){let o=new Set(t.flat(1/0).reverse());for(let r of o)e.unshift(Ht(r))}else t!==void 0&&e.push(Ht(t));return e}static _$Ep(t,e){let o=e.attribute;return o===!1?void 0:typeof o=="string"?o:typeof t=="string"?t.toLowerCase():void 0}_$Eu(){var t;this._$E_=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$Eg(),this.requestUpdate(),(t=this.constructor.h)===null||t===void 0||t.forEach(e=>e(this))}addController(t){var e,o;((e=this._$ES)!==null&&e!==void 0?e:this._$ES=[]).push(t),this.renderRoot!==void 0&&this.isConnected&&((o=t.hostConnected)===null||o===void 0||o.call(t))}removeController(t){var e;(e=this._$ES)===null||e===void 0||e.splice(this._$ES.indexOf(t)>>>0,1)}_$Eg(){this.constructor.elementProperties.forEach((t,e)=>{this.hasOwnProperty(e)&&(this._$Ei.set(e,this[e]),delete this[e])})}createRenderRoot(){var t;let e=(t=this.shadowRoot)!==null&&t!==void 0?t:this.attachShadow(this.constructor.shadowRootOptions);return ze(e,this.constructor.elementStyles),e}connectedCallback(){var t;this.renderRoot===void 0&&(this.renderRoot=this.createRenderRoot()),this.enableUpdating(!0),(t=this._$ES)===null||t===void 0||t.forEach(e=>{var o;return(o=e.hostConnected)===null||o===void 0?void 0:o.call(e)})}enableUpdating(t){}disconnectedCallback(){var t;(t=this._$ES)===null||t===void 0||t.forEach(e=>{var o;return(o=e.hostDisconnected)===null||o===void 0?void 0:o.call(e)})}attributeChangedCallback(t,e,o){this._$AK(t,o)}_$EO(t,e,o=Te){var r;let s=this.constructor._$Ep(t,o);if(s!==void 0&&o.reflect===!0){let a=(((r=o.converter)===null||r===void 0?void 0:r.toAttribute)!==void 0?o.converter:Le).toAttribute(e,o.type);this._$El=t,a==null?this.removeAttribute(s):this.setAttribute(s,a),this._$El=null}}_$AK(t,e){var o;let r=this.constructor,s=r._$Ev.get(t);if(s!==void 0&&this._$El!==s){let a=r.getPropertyOptions(s),h=typeof a.converter=="function"?{fromAttribute:a.converter}:((o=a.converter)===null||o===void 0?void 0:o.fromAttribute)!==void 0?a.converter:Le;this._$El=s,this[s]=h.fromAttribute(e,a.type),this._$El=null}}requestUpdate(t,e,o){let r=!0;t!==void 0&&(((o=o||this.constructor.getPropertyOptions(t)).hasChanged||ur)(this[t],e)?(this._$AL.has(t)||this._$AL.set(t,e),o.reflect===!0&&this._$El!==t&&(this._$EC===void 0&&(this._$EC=new Map),this._$EC.set(t,o))):r=!1),!this.isUpdatePending&&r&&(this._$E_=this._$Ej())}async _$Ej(){this.isUpdatePending=!0;try{await this._$E_}catch(e){Promise.reject(e)}let t=this.scheduleUpdate();return t!=null&&await t,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){var t;if(!this.isUpdatePending)return;this.hasUpdated,this._$Ei&&(this._$Ei.forEach((r,s)=>this[s]=r),this._$Ei=void 0);let e=!1,o=this._$AL;try{e=this.shouldUpdate(o),e?(this.willUpdate(o),(t=this._$ES)===null||t===void 0||t.forEach(r=>{var s;return(s=r.hostUpdate)===null||s===void 0?void 0:s.call(r)}),this.update(o)):this._$Ek()}catch(r){throw e=!1,this._$Ek(),r}e&&this._$AE(o)}willUpdate(t){}_$AE(t){var e;(e=this._$ES)===null||e===void 0||e.forEach(o=>{var r;return(r=o.hostUpdated)===null||r===void 0?void 0:r.call(o)}),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(t)),this.updated(t)}_$Ek(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$E_}shouldUpdate(t){return!0}update(t){this._$EC!==void 0&&(this._$EC.forEach((e,o)=>this._$EO(o,this[o],e)),this._$EC=void 0),this._$Ek()}updated(t){}firstUpdated(t){}};L[Pe]=!0,L.elementProperties=new Map,L.elementStyles=[],L.shadowRootOptions={mode:"open"},pr?.({ReactiveElement:L}),((Ce=Dt.reactiveElementVersions)!==null&&Ce!==void 0?Ce:Dt.reactiveElementVersions=[]).push("1.6.3");var Re,Mt=window,et=Mt.trustedTypes,br=et?et.createPolicy("lit-html",{createHTML:i=>i}):void 0,Oe="$lit$",U=`lit$${(Math.random()+"").slice(9)}$`,$r="?"+U,oi=`<${$r}>`,F=document,xt=()=>F.createComment(""),_t=i=>i===null||typeof i!="object"&&typeof i!="function",xr=Array.isArray,ri=i=>xr(i)||typeof i?.[Symbol.iterator]=="function",Ue=`[ 	
\f\r]`,$t=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,vr=/-->/g,gr=/>/g,W=RegExp(`>|${Ue}(?:([^\\s"'>=/]+)(${Ue}*=${Ue}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),mr=/'/g,fr=/"/g,_r=/^(?:script|style|textarea|title)$/i,wr=i=>(t,...e)=>({_$litType$:i,strings:t,values:e}),E=wr(1),$s=wr(2),Z=Symbol.for("lit-noChange"),$=Symbol.for("lit-nothing"),jr=new WeakMap,K=F.createTreeWalker(F,129,null,!1);function kr(i,t){if(!Array.isArray(i)||!i.hasOwnProperty("raw"))throw Error("invalid template strings array");return br!==void 0?br.createHTML(t):t}var ii=(i,t)=>{let e=i.length-1,o=[],r,s=t===2?"<svg>":"",a=$t;for(let h=0;h<e;h++){let n=i[h],l,p,d=-1,v=0;for(;v<n.length&&(a.lastIndex=v,p=a.exec(n),p!==null);)v=a.lastIndex,a===$t?p[1]==="!--"?a=vr:p[1]!==void 0?a=gr:p[2]!==void 0?(_r.test(p[2])&&(r=RegExp("</"+p[2],"g")),a=W):p[3]!==void 0&&(a=W):a===W?p[0]===">"?(a=r??$t,d=-1):p[1]===void 0?d=-2:(d=a.lastIndex-p[2].length,l=p[1],a=p[3]===void 0?W:p[3]==='"'?fr:mr):a===fr||a===mr?a=W:a===vr||a===gr?a=$t:(a=W,r=void 0);let g=a===W&&i[h+1].startsWith("/>")?" ":"";s+=a===$t?n+oi:d>=0?(o.push(l),n.slice(0,d)+Oe+n.slice(d)+U+g):n+U+(d===-2?(o.push(void 0),h):g)}return[kr(i,s+(i[e]||"<?>")+(t===2?"</svg>":"")),o]},wt=class i{constructor({strings:t,_$litType$:e},o){let r;this.parts=[];let s=0,a=0,h=t.length-1,n=this.parts,[l,p]=ii(t,e);if(this.el=i.createElement(l,o),K.currentNode=this.el.content,e===2){let d=this.el.content,v=d.firstChild;v.remove(),d.append(...v.childNodes)}for(;(r=K.nextNode())!==null&&n.length<h;){if(r.nodeType===1){if(r.hasAttributes()){let d=[];for(let v of r.getAttributeNames())if(v.endsWith(Oe)||v.startsWith(U)){let g=p[a++];if(d.push(v),g!==void 0){let k=r.getAttribute(g.toLowerCase()+Oe).split(U),w=/([.?@])?(.*)/.exec(g);n.push({type:1,index:s,name:w[2],strings:k,ctor:w[1]==="."?He:w[1]==="?"?De:w[1]==="@"?Me:rt})}else n.push({type:6,index:s})}for(let v of d)r.removeAttribute(v)}if(_r.test(r.tagName)){let d=r.textContent.split(U),v=d.length-1;if(v>0){r.textContent=et?et.emptyScript:"";for(let g=0;g<v;g++)r.append(d[g],xt()),K.nextNode(),n.push({type:2,index:++s});r.append(d[v],xt())}}}else if(r.nodeType===8)if(r.data===$r)n.push({type:2,index:s});else{let d=-1;for(;(d=r.data.indexOf(U,d+1))!==-1;)n.push({type:7,index:s}),d+=U.length-1}s++}}static createElement(t,e){let o=F.createElement("template");return o.innerHTML=t,o}};function ot(i,t,e=i,o){var r,s,a,h;if(t===Z)return t;let n=o!==void 0?(r=e._$Co)===null||r===void 0?void 0:r[o]:e._$Cl,l=_t(t)?void 0:t._$litDirective$;return n?.constructor!==l&&((s=n?._$AO)===null||s===void 0||s.call(n,!1),l===void 0?n=void 0:(n=new l(i),n._$AT(i,e,o)),o!==void 0?((a=(h=e)._$Co)!==null&&a!==void 0?a:h._$Co=[])[o]=n:e._$Cl=n),n!==void 0&&(t=ot(i,n._$AS(i,t.values),n,o)),t}var Ne=class{constructor(t,e){this._$AV=[],this._$AN=void 0,this._$AD=t,this._$AM=e}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(t){var e;let{el:{content:o},parts:r}=this._$AD,s=((e=t?.creationScope)!==null&&e!==void 0?e:F).importNode(o,!0);K.currentNode=s;let a=K.nextNode(),h=0,n=0,l=r[0];for(;l!==void 0;){if(h===l.index){let p;l.type===2?p=new kt(a,a.nextSibling,this,t):l.type===1?p=new l.ctor(a,l.name,l.strings,this,t):l.type===6&&(p=new Ie(a,this,t)),this._$AV.push(p),l=r[++n]}h!==l?.index&&(a=K.nextNode(),h++)}return K.currentNode=F,s}v(t){let e=0;for(let o of this._$AV)o!==void 0&&(o.strings!==void 0?(o._$AI(t,o,e),e+=o.strings.length-2):o._$AI(t[e])),e++}},kt=class i{constructor(t,e,o,r){var s;this.type=2,this._$AH=$,this._$AN=void 0,this._$AA=t,this._$AB=e,this._$AM=o,this.options=r,this._$Cp=(s=r?.isConnected)===null||s===void 0||s}get _$AU(){var t,e;return(e=(t=this._$AM)===null||t===void 0?void 0:t._$AU)!==null&&e!==void 0?e:this._$Cp}get parentNode(){let t=this._$AA.parentNode,e=this._$AM;return e!==void 0&&t?.nodeType===11&&(t=e.parentNode),t}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(t,e=this){t=ot(this,t,e),_t(t)?t===$||t==null||t===""?(this._$AH!==$&&this._$AR(),this._$AH=$):t!==this._$AH&&t!==Z&&this._(t):t._$litType$!==void 0?this.g(t):t.nodeType!==void 0?this.$(t):ri(t)?this.T(t):this._(t)}k(t){return this._$AA.parentNode.insertBefore(t,this._$AB)}$(t){this._$AH!==t&&(this._$AR(),this._$AH=this.k(t))}_(t){this._$AH!==$&&_t(this._$AH)?this._$AA.nextSibling.data=t:this.$(F.createTextNode(t)),this._$AH=t}g(t){var e;let{values:o,_$litType$:r}=t,s=typeof r=="number"?this._$AC(t):(r.el===void 0&&(r.el=wt.createElement(kr(r.h,r.h[0]),this.options)),r);if(((e=this._$AH)===null||e===void 0?void 0:e._$AD)===s)this._$AH.v(o);else{let a=new Ne(s,this),h=a.u(this.options);a.v(o),this.$(h),this._$AH=a}}_$AC(t){let e=jr.get(t.strings);return e===void 0&&jr.set(t.strings,e=new wt(t)),e}T(t){xr(this._$AH)||(this._$AH=[],this._$AR());let e=this._$AH,o,r=0;for(let s of t)r===e.length?e.push(o=new i(this.k(xt()),this.k(xt()),this,this.options)):o=e[r],o._$AI(s),r++;r<e.length&&(this._$AR(o&&o._$AB.nextSibling,r),e.length=r)}_$AR(t=this._$AA.nextSibling,e){var o;for((o=this._$AP)===null||o===void 0||o.call(this,!1,!0,e);t&&t!==this._$AB;){let r=t.nextSibling;t.remove(),t=r}}setConnected(t){var e;this._$AM===void 0&&(this._$Cp=t,(e=this._$AP)===null||e===void 0||e.call(this,t))}},rt=class{constructor(t,e,o,r,s){this.type=1,this._$AH=$,this._$AN=void 0,this.element=t,this.name=e,this._$AM=r,this.options=s,o.length>2||o[0]!==""||o[1]!==""?(this._$AH=Array(o.length-1).fill(new String),this.strings=o):this._$AH=$}get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}_$AI(t,e=this,o,r){let s=this.strings,a=!1;if(s===void 0)t=ot(this,t,e,0),a=!_t(t)||t!==this._$AH&&t!==Z,a&&(this._$AH=t);else{let h=t,n,l;for(t=s[0],n=0;n<s.length-1;n++)l=ot(this,h[o+n],e,n),l===Z&&(l=this._$AH[n]),a||(a=!_t(l)||l!==this._$AH[n]),l===$?t=$:t!==$&&(t+=(l??"")+s[n+1]),this._$AH[n]=l}a&&!r&&this.j(t)}j(t){t===$?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,t??"")}},He=class extends rt{constructor(){super(...arguments),this.type=3}j(t){this.element[this.name]=t===$?void 0:t}},si=et?et.emptyScript:"",De=class extends rt{constructor(){super(...arguments),this.type=4}j(t){t&&t!==$?this.element.setAttribute(this.name,si):this.element.removeAttribute(this.name)}},Me=class extends rt{constructor(t,e,o,r,s){super(t,e,o,r,s),this.type=5}_$AI(t,e=this){var o;if((t=(o=ot(this,t,e,0))!==null&&o!==void 0?o:$)===Z)return;let r=this._$AH,s=t===$&&r!==$||t.capture!==r.capture||t.once!==r.once||t.passive!==r.passive,a=t!==$&&(r===$||s);s&&this.element.removeEventListener(this.name,this,r),a&&this.element.addEventListener(this.name,this,t),this._$AH=t}handleEvent(t){var e,o;typeof this._$AH=="function"?this._$AH.call((o=(e=this.options)===null||e===void 0?void 0:e.host)!==null&&o!==void 0?o:this.element,t):this._$AH.handleEvent(t)}},Ie=class{constructor(t,e,o){this.element=t,this.type=6,this._$AN=void 0,this._$AM=e,this.options=o}get _$AU(){return this._$AM._$AU}_$AI(t){ot(this,t)}};var yr=Mt.litHtmlPolyfillSupport;yr?.(wt,kt),((Re=Mt.litHtmlVersions)!==null&&Re!==void 0?Re:Mt.litHtmlVersions=[]).push("2.8.0");var Ar=(i,t,e)=>{var o,r;let s=(o=e?.renderBefore)!==null&&o!==void 0?o:t,a=s._$litPart$;if(a===void 0){let h=(r=e?.renderBefore)!==null&&r!==void 0?r:null;s._$litPart$=a=new kt(t.insertBefore(xt(),h),h,void 0,e??{})}return a._$AI(i),a};var Be,qe;var x=class extends L{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){var t,e;let o=super.createRenderRoot();return(t=(e=this.renderOptions).renderBefore)!==null&&t!==void 0||(e.renderBefore=o.firstChild),o}update(t){let e=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(t),this._$Do=Ar(e,this.renderRoot,this.renderOptions)}connectedCallback(){var t;super.connectedCallback(),(t=this._$Do)===null||t===void 0||t.setConnected(!0)}disconnectedCallback(){var t;super.disconnectedCallback(),(t=this._$Do)===null||t===void 0||t.setConnected(!1)}render(){return Z}};x.finalized=!0,x._$litElement$=!0,(Be=globalThis.litElementHydrateSupport)===null||Be===void 0||Be.call(globalThis,{LitElement:x});var Sr=globalThis.litElementPolyfillSupport;Sr?.({LitElement:x});((qe=globalThis.litElementVersions)!==null&&qe!==void 0?qe:globalThis.litElementVersions=[]).push("3.3.3");var It=class extends x{#t;static get styles(){return A`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return E`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M6.53 5.47a.75.75 0 0 0-1.06 1.06L10.94 12l-5.47 5.47a.75.75 0 0 0 1.06 1.06L12 13.06l5.47 5.47a.75.75 0 0 0 1.06-1.06L13.06 12l5.47-5.47a.75.75 0 1 0-1.06-1.06L12 10.94 6.53 5.47Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-xmark",It);var ai=0,Er="open",Ve=class extends m{#t;static get styles(){return j`
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
    `}static get properties(){return{flipDisabled:{type:Boolean,attribute:"flip-disabled"},label:{type:String},open:{type:Boolean,reflect:!0},position:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.flipDisabled=!1,this.label=null,this.open=!1,this.position="top-center"}connectedCallback(){super.connectedCallback(),this.id=`tooltip-describedby-${ai++}`;let t=new MutationObserver(this.#e.bind(this)),e={childList:!0};t.observe(this.shadowRoot,e)}#e(t){for(let e of t)Array.from(e.addedNodes).some(s=>s.tagName==="SPAN")&&(this.#t.role="tooltip",this.addEventListener("focus",this.#i,!0),this.addEventListener("mouseenter",this.#i),this.addEventListener("blur",this.#s,!0),this.addEventListener("mouseleave",this.#s),this.addEventListener("keydown",this.#r)),e.removedNodes.length>0&&(this.#t.role="",this.removeEventListener("focus",this.#i),this.removeEventListener("mouseenter",this.#i),this.removeEventListener("blur",this.#s),this.removeEventListener("mouseleave",this.#s),this.removeEventListener("keydown",this.#r))}disconnectedCallback(){super.disconnectedCallback(),this.removeEventListener("focus",this.#i),this.removeEventListener("mouseenter",this.#i),this.removeEventListener("blur",this.#s),this.removeEventListener("mouseleave",this.#s),this.removeEventListener("keydown",this.#r)}#o(){let t=this.getAttribute("id");this.firstElementChild.setAttribute("aria-describedby",t);let e=window.getComputedStyle(this.firstElementChild).display;this.style.setProperty("display",e)}updated(){this.open&&this.#a()}#r(t){t.key==="Escape"&&this.#s()}#i(){this.setAttribute(Er,"")}#s(){this.removeAttribute(Er)}#a(){let t=this.getAttribute("position");if(["left","right","top-start","top-end","top-center","bottom-start","bottom-end","bottom-center"].includes(t)&&this.flipDisabled===!1&&this.label){let[e,o]=t.split("-"),r=this.#h(),s=n=>t===n,a=n=>e===n.split("-")[0],h=n=>{let[l,p]=n.split("-");return t==="left"&&n==="right"||t==="right"&&n==="left"||e==="bottom"&&l==="top"&&o===p||e==="top"&&l==="bottom"&&o===p};if(!r.some(s)){let n=r.find(a)||r.find(h)||r[0];this.setAttribute("position",n)}}}#h(){let{tooltipWidth:t,tooltipHeight:e,elemHeight:o,elemWidth:r}=this.#c(),{elemLeft:s,elemRight:a,elemTop:h,elemBottom:n}=this.#n(),l=h-e>0,p=n-e>0,d=a-t>0,v=s-t>0,g=this.#l(h,o,e),k=this.#l(n,o,e),w=window.innerWidth-s-t>0,lo=s>0,ho=this.#l(a,r,t),co=this.#l(s,r,t),po=a>0,uo=s+r-t>0;return Object.entries({left:v&&g&&k,right:d&&g&&k,"top-start":l&&w&&lo,"bottom-start":p&&w&&lo,"top-center":l&&ho&&co,"bottom-center":p&&ho&&co,"top-end":l&&po&&uo,"bottom-end":p&&po&&uo}).reduce((bo,[Rr,Ur])=>Ur?[...bo,Rr]:bo,[])}#c(){return{tooltipWidth:this.shadowRoot.querySelector("span").getBoundingClientRect().width,tooltipHeight:this.shadowRoot.querySelector("span").getBoundingClientRect().height,elemHeight:this.getBoundingClientRect().height,elemWidth:this.getBoundingClientRect().width}}#n(){return{elemLeft:this.getBoundingClientRect().left,elemRight:window.innerWidth-this.getBoundingClientRect().right,elemTop:this.getBoundingClientRect().top,elemBottom:window.innerHeight-this.getBoundingClientRect().bottom}}#l(t,e,o){return t+e/2-o/2>0}render(){let t;return this.label&&(t=c`
        <span
          class=${b(this.open?"show":null)}
          aria-hidden=${this.open?"false":"true"}
        >
          ${this.label}
        </span>
      `),c`
      <slot @slotchange=${this.#o}></slot>
      ${t}
    `}};customElements.define("jh-tooltip",Ve);var We=class extends m{#t=!1;#e;static get styles(){return j`
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
    `}static get properties(){return{label:{type:String},dismissButtonAccessibleLabel:{type:String,attribute:"dismiss-button-accessible-label"},dismissible:{type:Boolean},tooltipLabel:{type:String,attribute:"tooltip-label"},size:{type:String,reflect:!0},href:{type:String},target:{type:String},removeOnDismiss:{type:Boolean,attribute:"remove-on-dismiss"}}}constructor(){super(),this.label=null,this.dismissible=!1,this.dismissButtonAccessibleLabel=null,this.tooltipLabel=null,this.size="small",this.href=null,this.target=null,this.removeOnDismiss=!1,this.addEventListener("keyup",this.#i),this.addEventListener("keyup",this.#o),this.addEventListener("blur",this.#s)}connectedCallback(){super.connectedCallback(),new ResizeObserver(this.#r.bind(this)).observe(this)}#o(t){(t.code==="Space"||t.code==="Enter")&&this.click()}#r(){if(!this.label)return;let t=this.#t,e=this.shadowRoot.querySelector(".label");this.#t=e.offsetWidth<e.scrollWidth,t!==this.#t&&(this.#t&&!this.href?(this.#e=this.getAttribute("tabindex"),this.setAttribute("tabindex","0")):this.#t||(this.#e?this.setAttribute("tabindex",this.#e):this.removeAttribute("tabindex")),this.update())}#i(t){if(t.target.#t&&(t.key==="Tab"||t.shiftKey&&t.key==="Tab")){let e=this.shadowRoot.querySelector("jh-tooltip");(t.composedPath()[0].localName==="jh-tag"||t.composedPath()[0].localName==="a")&&(e.open=!e.open),t.composedPath()[0].localName==="button"&&(e.open=!1)}}#s(t){if(t.target.#t){let e=this.shadowRoot.querySelector("jh-tooltip");e.open=!1}}#a(t){t&&(this.shadowRoot.querySelector('slot[name="jh-tag-icon"]').classList.add("display-icon-slot"),this.firstElementChild.setAttribute("size","x-small"))}#h(t){t.stopPropagation(),this.dispatchEvent(new CustomEvent("jh-dismiss",{bubbles:!0,cancelable:!0,composed:!0})),this.removeOnDismiss&&this.remove()}#c(){let t;return this.dismissible&&(t=c`
      <jh-button
        size="medium"
        appearance="secondary"
        accessible-label=${this.dismissButtonAccessibleLabel}
        @click=${this.#h}>
        <slot name="jh-tag-dismiss-icon" slot="jh-button-icon">
          <jh-icon-xmark slot="jh-button-icon"></jh-icon-xmark>
        </slot>
      </jh-button>
      `),this.dismissible&&this.tooltipLabel&&(t=c`<jh-tooltip label=${this.tooltipLabel}>${t}</jh-tooltip>`),t}#n(){let t,e=c`<span class="label">${this.label}</span>`;return this.#t&&(e=c`<jh-tooltip label=${this.label}>${e}</jh-tooltip>`),this.href?(this.removeAttribute("tabindex"),t=c` <a class="label-container" tabindex="0" href=${this.href}>${e}</a>`):t=c` <div class="label-container">${e}</div>`,t}render(){return c` 
    <span class="container">
      <slot name="jh-tag-icon" @slotchange=${this.#a}></slot>
      ${this.#n()}
      ${this.#c()}
    </span>
    `}};customElements.define("jh-tag",We);var Bt=class extends x{#t;static get styles(){return A`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return E`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M4.625 10.375a5.75 5.75 0 1 1 11.5 0 5.75 5.75 0 0 1-11.5 0Zm5.75-7.25a7.25 7.25 0 1 0 4.569 12.88l4.65 4.65a.75.75 0 1 0 1.061-1.06l-4.65-4.651a7.25 7.25 0 0 0-5.63-11.819Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-magnifying-glass",Bt);var qt=class extends x{#t;static get styles(){return A`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return E`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M12 3.75a8.25 8.25 0 1 0 0 16.5 8.25 8.25 0 0 0 0-16.5ZM2.25 12c0-5.384 4.365-9.75 9.75-9.75s9.75 4.366 9.75 9.75c0 5.385-4.365 9.75-9.75 9.75S2.25 17.386 2.25 12Zm6.22-3.53a.75.75 0 0 1 1.06 0L12 10.94l2.47-2.47a.75.75 0 1 1 1.06 1.06L13.06 12l2.47 2.47a.75.75 0 1 1-1.06 1.06L12 13.062l-2.47 2.47a.75.75 0 0 1-1.06-1.06L10.94 12 8.47 9.53a.75.75 0 0 1 0-1.06Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-circle-xmark",qt);var Vt=class extends x{#t;static get styles(){return A`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return E`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M12 6.25c-4.354 0-6.76 2.428-9.066 4.754l-.464.466a.75.75 0 0 0 0 1.061l.464.467C5.24 15.324 7.646 17.751 12 17.751c4.354 0 6.76-2.427 9.066-4.753l.464-.467a.75.75 0 0 0 0-1.06l-.464-.467C18.76 8.678 16.354 6.25 12 6.25Zm0 10c-3.65 0-5.635-1.932-7.941-4.25C6.365 9.685 8.349 7.75 12 7.75c3.65 0 5.635 1.934 7.941 4.25-2.306 2.318-4.29 4.25-7.941 4.25ZM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-eye",Vt);var Wt=class extends x{#t;static get styles(){return A`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return E`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="m4.672 4.673.708-.707 2.956 2.956c1.06-.42 2.261-.671 3.664-.671 4.354 0 6.76 2.427 9.066 4.753l.464.466a.75.75 0 0 1 0 1.061l-.464.467c-1.094 1.103-2.21 2.23-3.546 3.108l2.515 2.515-.707.708L4.672 4.673ZM6.48 7.895c-1.336.88-2.452 2.005-3.546 3.109l-.464.466a.75.75 0 0 0 0 1.061l.464.467C5.24 15.324 7.646 17.75 12 17.75c1.403 0 2.604-.252 3.664-.672l-1.176-1.176A8.57 8.57 0 0 1 12 16.25c-3.65 0-5.635-1.933-7.941-4.25 1.166-1.172 2.25-2.245 3.507-3.02L6.48 7.895Zm2.812 2.812a3 3 0 0 0 4 4.001l-4-4Zm.22-2.609 1.195 1.195a3 3 0 0 1 4 4l1.727 1.727c1.258-.775 2.341-1.848 3.507-3.02-2.306-2.316-4.29-4.25-7.941-4.25a8.57 8.57 0 0 0-2.488.348Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-eye-slash",Wt);var ni=0,Ke=class extends m{static get formAssociated(){return!0}#t;#e;#o;static get styles(){return j`
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
    `}static get properties(){return{accessibleLabelClearButton:{type:String,attribute:"accessible-label-clear-button"},accessibleLabelInput:{type:String,attribute:"accessible-label-input"},accessibleLabelHidePassword:{type:String,attribute:"accessible-label-hide-password"},accessibleLabelShowPassword:{type:String,attribute:"accessible-label-show-password"},autocomplete:{type:String},autoGrow:{type:Boolean,attribute:"auto-grow",reflect:!0},cols:{type:String},disabled:{type:Boolean,reflect:!0},enterkeyhint:{type:String},errorText:{type:String,attribute:"error-text"},helperText:{type:String,attribute:"helper-text",reflect:!0},hideLeftSlot:{type:Boolean,attribute:"hide-left-slot",reflect:!0},passwordVisible:{type:Boolean,attribute:"password-visible"},hideRightSlot:{type:Boolean,attribute:"hide-right-slot",reflect:!0},inputmode:{type:String},invalid:{type:Boolean,reflect:!0},label:{type:String},maxlength:{type:String},minlength:{type:String},name:{type:String},noResize:{type:Boolean,attribute:"no-resize",reflect:!0},placeholder:{type:String},readonly:{type:Boolean,reflect:!0},required:{type:Boolean,reflect:!0},rows:{type:String},showClearButton:{type:Boolean,attribute:"show-clear-button",reflect:!0},showIndicator:{type:Boolean,attribute:"show-indicator",reflect:!0},size:{type:String,reflect:!0},spellcheck:{type:String},type:{type:String,reflect:!0},value:{type:String},wrap:{type:String}}}constructor(){super(),this.#t=this.attachInternals(),this.accessibleLabelInput=null,this.accessibleLabelClearButton=null,this.accessibleLabelHidePassword=null,this.accessibleLabelShowPassword=null,this.autocomplete=null,this.autoGrow=!1,this.cols=null,this.disabled=!1,this.enterkeyhint=null,this.errorText=null,this.helperText=null,this.hideLeftSlot=!1,this.passwordVisible=!1,this.hideRightSlot=!1,this.inputmode=null,this.invalid=!1,this.label=null,this.maxlength=null,this.minlength=null,this.name=null,this.noResize=!1,this.placeholder=null,this.readonly=!1,this.required=!1,this.rows=null,this.showClearButton=!1,this.showIndicator=!1,this.size="medium",this.spellcheck=null,this.type="text",this.value=null,this.wrap=null}get form(){return this.#t.form}get validity(){return this.#t.validity}get value(){return this.#e}set value(t){let e=this.#e;t!==e&&(this.#e=t,this.#t.setFormValue(this.#e)),this.requestUpdate("value",e)}connectedCallback(){super.connectedCallback(),this.#o=ni++}#r(){this.value=null,this.shadowRoot.querySelector("input").focus(),this.#i("jh-clear")}#i(t){this.dispatchEvent(new CustomEvent(t,{bubbles:!0,cancelable:!0,composed:!0}))}#s(){!this.readonly&&!this.disabled&&(this.passwordVisible=!this.passwordVisible)}#a(){let t=!1,e=!1;for(let o of this.children)o.slot==="jh-input-left"?o.assignedSlot!==null&&(t=!0):(o.slot==="jh-input-right"||o.slot==="jh-input-password-visible"||o.slot==="jh-input-password-hidden")&&o.assignedSlot!==null&&(e=!0);this.#h(t,e)}#h(t,e){let o,r=this.shadowRoot.querySelector("input");t&&e?o="input-both-slots":t?o="input-left-slot":e&&(o="input-right-slot"),o?(r.className&&r.classList.remove(r.className),r.classList.add(o)):r.classList.remove(r.className)}#c(){let t=this.shadowRoot.querySelector("textarea");t.style.height="auto",t.style.height=t.scrollHeight+"px"}#n(){this.#i("jh-change")}#l(t){this.value=t.target.value,this.type==="textarea"&&this.autoGrow&&this.#c()}#d(t){let e=t.target.value.substring(t.target.selectionStart,t.target.selectionEnd);this.dispatchEvent(new CustomEvent("jh-select",{detail:{selected:e},bubbles:!0,cancelable:!0,composed:!0}))}#p(){return c`
      <slot
        class="left-slot"
        name="jh-input-left"
        @slotchange=${this.#a}
        >${this.type==="search"?c`
              <jh-icon-magnifying-glass aria-hidden="true" size="small"></jh-icon-magnifying-glass>
            `:null}
      </slot>
    `}#u(){let t=this.value?null:"hide-clear-button";return c`
      <jh-button size="small" appearance="tertiary" class="clear-button ${t}" 
      accessible-label=${b(this.accessibleLabelClearButton)} 
      ?disabled=${this.disabled||this.readonly} 
      @click=${this.#r}
      >
        <slot name="jh-input-clear-button" slot="jh-button-icon">
          <jh-icon-circle-xmark slot="jh-button-icon" aria-hidden="true" size="small"></jh-icon-circle-xmark>
        </slot>
      </jh-button>
    `}#b(){if(this.type==="password"){let t=this.passwordVisible?this.accessibleLabelHidePassword:this.accessibleLabelShowPassword;return c`
        <jh-button
          size="small"
          class="right-slot"
          appearance="tertiary"
          ?disabled=${this.disabled||this.readonly}
          accessible-label=${b(t)}
          @click=${this.#s}
        >
          ${this.passwordVisible?c`
                <slot
                  name="jh-input-password-visible"
                  slot="jh-button-icon"
                  @slotchange=${this.#a}
                >
                  <jh-icon-eye-slash
                    slot="jh-button-icon"
                    aria-hidden="true"
                    size="small"
                  ></jh-icon-eye-slash>
                </slot>
              `:c`
                <slot
                  name="jh-input-password-hidden"
                  slot="jh-button-icon"
                  @slotchange=${this.#a}
                >
                  <jh-icon-eye
                    slot="jh-button-icon"
                    aria-hidden="true"
                    size="small"
                  ></jh-icon-eye>
                </slot>
              `}
        </jh-button>
      `}else return c`
        <slot
          class="right-slot"
          name="jh-input-right"
          @slotchange=${this.#a}
        ></slot>
      `}#v(){if(this.readonly)return;let t=this.showClearButton&&!this.readonly&&!this.disabled,e=this.hideLeftSlot?null:this.#p(),o=t?this.#u():null,r;return this.hideRightSlot||this.type==="password"&&this.disabled?r=null:r=this.#b(),c`${e}${o}${r}`}#g(){if(this.label&&this.helperText&&this.errorText)return`jh-input-error-${this.#o} jh-input-desc-${this.#o}`;if(this.label&&this.helperText)return`jh-input-desc-${this.#o}`;if(this.invalid&&this.errorText)return`jh-input-error-${this.#o}`}render(){let t=["email","password","search","tel","text","textarea","url"],e,o,r,s,a,h,n=this.label&&this.helperText||this.invalid&&this.errorText?this.#g():null;return this.showIndicator&&(this.required?r=c`<span class="indicator" aria-hidden="true"> *</span>`:r=c`<span class="indicator"> (optional)</span>`),this.helperText&&(s=c`
        <p id="jh-input-desc-${this.#o}" class="helper-text">
          ${this.helperText}
        </p>
      `),this.label&&(e=c`
        <label for="jh-input-${this.#o}"> ${this.label}${r} </label>
        ${s}
      `),this.invalid&&this.errorText&&(a=c`
        <div id="jh-input-error-${this.#o}" class="error-text">
          ${this.errorText}
        </div>
      `),t.includes(this.type)&&(this.type==="password"&&this.passwordVisible?h="text":h=this.type),this.type==="textarea"?o=c`
        <textarea
          id="jh-input-${this.#o}"
          aria-describedby=${b(n)}
          aria-invalid=${b(this.invalid?"true":null)}
          aria-label=${b(this.accessibleLabelInput===""?null:this.accessibleLabelInput)}
          autocomplete=${b(this.autocomplete===""?null:this.autocomplete)}
          ?disabled=${this.disabled}
          inputmode=${b(this.inputmode===""?null:this.inputmode)}
          ?invalid=${this.invalid}
          maxlength=${b(this.maxlength===""?null:this.maxlength)}
          minlength=${b(this.minlength===""?null:this.minlength)}
          name=${b(this.name===""?null:this.name)}
          placeholder=${b(this.placeholder===""?null:this.placeholder)}
          ?readonly=${this.readonly}
          ?required=${this.required}
          spellcheck=${b(this.spellcheck===""?null:this.spellcheck)}
          .value=${this.value}
          @change=${this.#n}
          @input=${this.#l}
          @select=${this.#d}
          cols=${b(this.cols===""?null:this.cols)}
          rows=${b(this.rows===""?null:this.rows)}
          wrap=${b(this.wrap===""?null:this.wrap)}
        ></textarea>
      `:o=c`
        <div class="input-container">
          <input
            id="jh-input-${this.#o}"
            aria-describedby=${b(n)}
            aria-invalid=${b(this.invalid?"true":null)}
            aria-label=${b(this.accessibleLabelInput===""?null:this.accessibleLabelInput)}
            autocomplete=${b(this.autocomplete===""?null:this.autocomplete)}
            ?disabled=${this.disabled}
            inputmode=${b(this.inputmode===""?null:this.inputmode)}
            ?invalid=${this.invalid}
            maxlength=${b(this.maxlength===""?null:this.maxlength)}
            minlength=${b(this.minlength===""?null:this.minlength)}
            name=${b(this.name===""?null:this.name)}
            placeholder=${b(this.placeholder===""?null:this.placeholder)}
            ?readonly=${this.readonly}
            ?required=${this.required}
            spellcheck=${b(this.spellcheck===""?null:this.spellcheck)}
            .value=${this.value}
            @change=${this.#n}
            @input=${this.#l}
            @select=${this.#d}
            type=${b(h)}
          />${this.#v()}
        </div>
      `,c` ${e} ${o} ${a} `}};customElements.define("jh-input",Ke);var li=0,Fe=class extends m{static get formAssociated(){return!0}#t;#e;#o;#r;static get styles(){return j`
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
    `}static get properties(){return{accessibleLabel:{type:String,attribute:"accessible-label"},errorText:{type:String,attribute:"error-text"},helperText:{attribute:"helper-text"},invalid:{type:Boolean,reflect:!0},label:{type:String},name:{type:String},required:{type:Boolean,reflect:!0},orientation:{type:String,reflect:!0},showIndicator:{type:Boolean,reflect:!0,attribute:"show-indicator"},value:{type:String,reflect:!0}}}constructor(){super(),this.#o=this.attachInternals(),this.accessibleLabel=null,this.errorText=null,this.helperText=null,this.invalid=!1,this.label=null,this.name=null,this.required=!1,this.orientation="vertical",this.showIndicator=!1,this.value=null,this.addEventListener("jh-change",this.#a),this.addEventListener("keydown",this.#h),this.addEventListener("focusout",this.#c)}connectedCallback(){super.connectedCallback(),this.#e=li++}get form(){return this.#o.form}get validity(){return this.#o.validity}get value(){return this.#r}set value(t){let e=this.#r;t!==e&&(this.#r=t,this.#o.setFormValue(t)),this.requestUpdate("value",e)}#i(){return[...this.querySelectorAll("jh-radio")]}#s(){let t=this.#i();if(this.#t=t.find(e=>e.value===this.value&&this.value!==null),!this.#t){let e=t.find(o=>o.checked);e?(this.value=e.value,this.#t=e):(this.value=null,this.#t=null)}this.#n(),this.#t||(t[0].tabIndex=0)}#a(t){if(t.target.tagName==="JH-RADIO"){this.value=t.target.value,this.#t=t.target,this.#n();let e={bubbles:!0,composed:!0,cancelable:!0};this.dispatchEvent(new CustomEvent("jh-change",e))}}#h(t){if(!["ArrowDown","ArrowRight","ArrowUp","ArrowLeft","Space"].includes(t.code))return;let o=this.#i().filter(a=>!a.disabled),r=o.length,s=o.findIndex(a=>a.checked);s=s===-1?0:s,t.code==="ArrowDown"||t.code==="ArrowRight"?(t.preventDefault(),s+1<r?s++:s=0):(t.code==="ArrowUp"||t.code==="ArrowLeft")&&(t.preventDefault(),s-1>=0?s--:s=r-1),this.value=o[s].value,o[s].click(),this.#t=o[s],this.#n(),o[s].focus()}#c(){let t=this.#i().filter(e=>!e.disabled);t.some(e=>e.checked)&&t.forEach(e=>e.checked?e.tabIndex=0:e.tabIndex=-1)}#n(){this.#i().forEach(e=>{e.checked=this.#t===e,e.checked?e.tabIndex=0:e.tabIndex=-1})}#l(){if(this.errorText&&this.invalid&&this.helperText&&this.label)return`radio-group-error-${this.#e} radio-group-helper-${this.#e}`;if(this.errorText&&this.invalid)return`radio-group-error-${this.#e}`;if(this.helperText&&this.label)return`radio-group-helper-${this.#e}`}render(){let t,e,o,r;return this.showIndicator&&(this.required?t=c`<span class="indicator" aria-hidden="true"> *</span>`:t=c`<span class="indicator"> (optional)</span>`),this.helperText&&(e=c`<p
        class="helper-text"
        id="radio-group-helper-${this.#e}"
      >
        ${this.helperText}
      </p>`),this.label&&(o=c`<legend class="label" for="radio-group-label-${this.#e}">
          ${this.label}${t}
        </legend>
        ${e}`),this.invalid&&this.errorText&&(r=c`<p class="error-text" id="radio-group-error-${this.#e}">
        ${this.errorText}
      </p>`),c`
      <fieldset
        role="radiogroup"
        id=${b(this.label?`radio-group-label-${this.#e}`:null)}
        aria-describedby=${b(this.#l())}
        aria-required=${b(this.required?"true":"false")}
        aria-invalid=${b(this.invalid?"true":null)}
        aria-label=${b(this.accessibleLabel)}
      >
        ${o}
        <div class="controls">
          <slot @slotchange=${this.#s}></slot>
        </div>
        ${r}
      </fieldset>
    `}};customElements.define("jh-radio-group",Fe);var hi=0,Ze=class extends m{#t;#e;static get styles(){return j`
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
    `}static get properties(){return{accessibleLabel:{type:String,attribute:"accessible-label"},checked:{type:Boolean,reflect:!0},disabled:{type:Boolean,reflect:!0},helperText:{type:String,attribute:"helper-text"},label:{type:String},value:{type:String}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="radio",this.accessibleLabel=null,this.checked=!1,this.disabled=!1,this.helperText=null,this.label=null,this.value=null,this.addEventListener("click",this.#r),this.addEventListener("keydown",this.#i)}connectedCallback(){super.connectedCallback(),this.#t=hi++,this.setAttribute("tabindex","0"),this.setAttribute("aria-checked",`${this.checked}`),this.accessibleLabel&&this.setAttribute("aria-label",`${this.accessibleLabel}`),this.helperText&&this.setAttribute("aria-describedby",`radio-helper-text-${this.#t}`);let t=new MutationObserver(this.#o.bind(this)),e={attributes:!0};t.observe(this,e)}#o(t){for(let e of t)e.attributeName==="checked"&&this.setAttribute("aria-checked",`${this.checked}`),e.attributeName==="disabled"&&this.setAttribute("aria-disabled",`${this.disabled}`),e.attributeName==="accessible-label"&&this.setAttribute("aria-label",`${this.accessibleLabel}`)}#r(){if(this.disabled||this.checked)return;this.checked=!0;let t={bubbles:!0,composed:!0,cancelable:!0};this.dispatchEvent(new CustomEvent("jh-change",t))}#i(t){t.code==="Space"&&this.#r()}render(){let t,e;return this.helperText&&(t=c`
        <p class="helper-text" id="radio-helper-text-${this.#t}">
          ${this.helperText}
        </p>
      `),this.label&&(e=c`
        <span class="label-container">
          <span class="label-text">${this.label}</span>
          ${t}
        </span>
      `),c`
      <span class="radio" aria-hidden="true"></span>
      ${e}
    `}};customElements.define("jh-radio",Ze);var Ge=class extends m{static get styles(){return j`
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
    `}static get properties(){return{appearance:{type:String,reflect:!0},dismissButtonAccessibleLabel:{type:String,attribute:"dismiss-button-accessible-label"},hideDismissButton:{type:Boolean,attribute:"hide-dismiss-button"},stacked:{type:Boolean,reflect:!0},type:{type:String,reflect:!0}}}constructor(){super(),this.appearance="neutral",this.dismissButtonAccessibleLabel=null,this.hideDismissButton=!1,this.stacked=!1,this.type="alert"}#t(){this.#e("jh-dismiss"),this.parentNode.host?.nodeName!=="JH-TOAST"&&this.remove()}#e(t){this.dispatchEvent(new CustomEvent(t,{bubbles:!0,cancelable:!0,composed:!0}))}#o(){let t=this.stacked?"stacked-action-btn":"inline-action-btn";return c`
        <slot name="jh-notification-action" class=${t} @slotchange=${this.#r}></slot>
    `}#r(){let t=this.querySelectorAll("jh-button");for(let e of t)e.getAttribute("size")!=="small"&&e.setAttribute("size","small")}#i(t){t&&this.shadowRoot.querySelector("slot:not([name])").classList.add("display-default-slot")}render(){let t;return this.hideDismissButton||(t=c`
        <jh-button
          size="small"
          appearance="secondary"
          accessible-label=${this.dismissButtonAccessibleLabel}
          @click=${this.#t}
          ><slot name="jh-notification-dismiss-icon" slot="jh-button-icon">
            <jh-icon-xmark slot="jh-button-icon"></jh-icon-xmark>
          </slot>
        </jh-button>
      `),c`
      <div class=${this.stacked?"stacked-container":"inline-container"}>
        <slot aria-hidden="true" name="jh-notification-icon"></slot>
        <slot @slotchange=${this.#i}></slot>
        ${this.stacked?null:this.#o()}
        ${t}
      </div>
      ${this.stacked?this.#o():null}
    `}};customElements.define("jh-notification",Ge);var ci=0,Ye=class extends m{#t;static get styles(){return j`
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
    `}static get properties(){return{label:{type:String},accessibleLabel:{type:String,attribute:"accessible-label"}}}constructor(){super(),this.label=null,this.accessibleLabel=null}connectedCallback(){super.connectedCallback(),this.#t=ci++}render(){return c`
      ${this.label?c`<div class="subheader" id="list-group-labelledby-${this.#t}">
            ${this.label}
          </div>`:null}
      <div
        role="group"
        aria-labelledby=${b(this.label?`list-group-labelledby-${this.#t}`:null)}
        aria-label=${b(this.accessibleLabel)}
      >
        <slot></slot>
      </div>
    `}};customElements.define("jh-list-group",Ye);var Qe=class extends m{#t;static get styles(){return j`
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
    `}static get properties(){return{disabled:{type:Boolean,reflect:!0},dividerInset:{type:Number,attribute:"divider-inset"},primaryMetadata:{type:String,attribute:"primary-metadata"},primaryText:{type:String,attribute:"primary-text"},secondaryMetadata:{type:String,attribute:"secondary-metadata"},secondaryText:{type:String,attribute:"secondary-text"},selected:{type:Boolean,reflect:!0},showDivider:{type:Boolean,reflect:!0,attribute:"show-divider"}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="listitem",this.disabled=!1,this.dividerInset=null,this.primaryMetadata=null,this.primaryText=null,this.secondaryMetadata=null,this.secondaryText=null,this.selected=!1,this.showDivider=!1,this.primaryTooltip=!1,this.secondaryTooltip=!1}#e(){let t=this.primaryTooltip,e=this.secondaryTooltip,o=this.shadowRoot.querySelector(".primary-text"),r=this.shadowRoot.querySelector(".secondary-text");this.primaryTooltip=o.offsetWidth<o.scrollWidth,this.secondaryTooltip=r.offsetWidth<r.scrollWidth,(t!==this.primaryTooltip||e!==this.secondaryTooltip)&&this.update()}async firstUpdated(){let t=this.shadowRoot.querySelectorAll("jh-tooltip"),e={characterData:!0,subtree:!0};await t.forEach(o=>{Promise.all(Array.from(o.children).map(r=>r.updateComplete)),new MutationObserver(this.#e.bind(this)).observe(o.children[0],e),new ResizeObserver(this.#e.bind(this)).observe(o.children[0])})}render(){let t;return this.disabled===!0&&this.hasAttribute("tabindex")?this.setAttribute("aria-disabled","true"):this.removeAttribute("aria-disabled"),this.showDivider&&(t=c`
        <jh-divider
          inset=${b(this.dividerInset?this.dividerInset:null)}
        ></jh-divider>
      `),c`
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
      ${t}
    `}};customElements.define("jh-list-item",Qe);var zr=Symbol(),z=class{get taskComplete(){return this.t||(this.i===1?this.t=new Promise((t,e)=>{this.o=t,this.h=e}):this.i===3?this.t=Promise.reject(this.l):this.t=Promise.resolve(this.u)),this.t}constructor(t,e,o){this.p=0,this.i=0,(this._=t).addController(this);let r=typeof e=="object"?e:{task:e,args:o};this.v=r.task,this.j=r.args,this.m=r.argsEqual??Cr,this.k=r.onComplete,this.A=r.onError,this.autoRun=r.autoRun??!0,"initialValue"in r&&(this.u=r.initialValue,this.i=2,this.O=this.T?.())}hostUpdate(){this.autoRun===!0&&this.S()}hostUpdated(){this.autoRun==="afterUpdate"&&this.S()}T(){if(this.j===void 0)return;let t=this.j();if(!Array.isArray(t))throw Error("The args function must return an array");return t}async S(){let t=this.T(),e=this.O;this.O=t,t===e||t===void 0||e!==void 0&&this.m(e,t)||await this.run(t)}async run(t){let e,o;t??=this.T(),this.O=t,this.i===1?this.q?.abort():(this.t=void 0,this.o=void 0,this.h=void 0),this.i=1,this.autoRun==="afterUpdate"?queueMicrotask(()=>this._.requestUpdate()):this._.requestUpdate();let r=++this.p;this.q=new AbortController;let s=!1;try{e=await this.v(t,{signal:this.q.signal})}catch(a){s=!0,o=a}if(this.p===r){if(e===zr)this.i=0;else{if(s===!1){try{this.k?.(e)}catch{}this.i=2,this.o?.(e)}else{try{this.A?.(o)}catch{}this.i=3,this.h?.(o)}this.u=e,this.l=o}this._.requestUpdate()}}abort(t){this.i===1&&this.q?.abort(t)}get value(){return this.u}get error(){return this.l}get status(){return this.i}render(t){switch(this.i){case 0:return t.initial?.();case 1:return t.pending?.();case 2:return t.complete?.(this.value);case 3:return t.error?.(this.error);default:throw Error("Unexpected status: "+this.i)}}},Cr=(i,t)=>i===t||i.length===t.length&&i.every((e,o)=>!zt(e,t[o]));var it=class{events=[];connected=!1;constructor(t,{limit:e=100,onEvent:o}={}){this._host=t,this._limit=e,this._onEvent=o,t.addController(this)}hostConnected(){Mo().then(t=>{this.events=(t??[]).slice(-this._limit),this._host.requestUpdate()}).catch(()=>{}),this._es=new EventSource("/v1/events"),this._es.addEventListener("hello",()=>{this.connected=!0,this._host.requestUpdate()}),this._es.addEventListener("activity",t=>{let e=JSON.parse(t.data);this.events=[...this.events,e].slice(-this._limit),this._host.requestUpdate(),this._onEvent?.(e)}),this._es.onerror=()=>{this.connected=!1,this._host.requestUpdate()}}hostDisconnected(){this._es?.close(),this._es=void 0}};function Je(i){return i==null?"":i<90?`${i}s`:i<5400?`${Math.floor(i/60)}m`:i<172800?`${Math.floor(i/3600)}h`:`${Math.floor(i/86400)}d`}function di(i){let t="";try{t=atob(i.Body??"")}catch{return i.Subject??""}if(!t)return i.Subject??"";let e=null;try{e=JSON.parse(t)}catch{}if(e&&typeof e=="object")switch(i.Kind){case"gate":return`${e.Gate}: ${e.Decision}${e.Reason?" \u2014 "+Xe(e.Reason):""}`;case"agent-run":return`${e.seat} as ${e.role}, mode ${e.mode}`;case"agent-result":{let o=[`${e.stop}`,`${Math.round((e.wall_ms??0)/100)/10}s`];return o.push(e.metered?`${e.tokens} tokens`:"cost NOT reported"),o.push(e.tools?Tr(e.tools):`${e.tool_calls} tools`),e.denied_tools&&o.push(`DENIED ${Tr(e.denied_tools)}`),e.unknown_updates?.length&&o.push(`dialect drift: ${e.unknown_updates.join(", ")}`),o.join(" \xB7 ")}case"authz":return e.reason??"";default:return Xe(t)}return Xe(t)}function Xe(i){let t=String(i).indexOf(`
`);return t<0?String(i):String(i).slice(0,t)}function Tr(i){return Object.entries(i).sort((t,e)=>e[1]-t[1]).map(([t,e])=>e>1?`${t}\xD7${e}`:t).join(" ")}function Lr(i){return i==null?"0":i<1e3?String(i):i<1e6?`${Math.round(i/100)/10}k`:`${Math.round(i/1e5)/10}M`}function Pr(i){return i.route?i.key?`${i.route}/${i.key}`:i.route:i.ingress??""}var to={live:["pending","claimed","waiting"],pending:["pending"],claimed:["claimed"],waiting:["waiting"],parked:["parked"],done:["done"],all:[]};var eo=class extends _{static styles=S`
    :host { display: grid; gap: 16px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; }
    .stat { text-align: center; }
    .stat .n { font-size: 1.6rem; font-weight: 600; }
    .stat .l { color: var(--jh-color-content-secondary-enabled); font-size: .8rem; }
    .feed { max-height: 420px; overflow-y: auto; }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); }
    .idle { color: var(--jh-color-content-secondary-enabled); }
  `;_status=new z(this,{task:async(t,{signal:e})=>ae(e),args:()=>[]});_running=new z(this,{task:async(t,{signal:e})=>ne(to.live,e),args:()=>[]});_activity=new it(this,{onEvent:()=>this._refresh()});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>{this._status.run(),this._running.run()},400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}render(){return u`
      <jh-card header-title="Factory status" padding="medium">
        ${this._status.render({pending:()=>u`<jh-progress accessible-label="Loading status"></jh-progress>`,error:t=>u`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>u`
            <div class="grid">
              <div class="stat"><div class="n">${t.pending}</div><div class="l">pending</div></div>
              <div class="stat"><div class="n">${t.exceptions}</div><div class="l">exceptions</div></div>
              <div class="stat"><div class="n">${t.stopped?"stopped":"running"}</div><div class="l">claims</div></div>
              ${t.spend?u`
                <div class="stat">
                  <div class="n">${Lr(t.spend.tokens)}</div>
                  <div class="l">tokens${t.spend.max_tokens?` of ${Lr(t.spend.max_tokens)}`:""}</div>
                </div>
                <div class="stat">
                  <div class="n">${t.spend.running}/${t.spend.max_concurrent}</div>
                  <div class="l">running</div>
                </div>`:f}
              <div class="stat"><div class="n">${t.routes.length}</div><div class="l">routes</div></div>
            </div>
            ${""}
            ${t.spend?.unmetered?u`<jh-notification appearance="negative" type="banner" hide-dismiss-button>
                  ${t.spend.unmetered} run(s) this window did not report a cost — the spend above is
                  incomplete and claims are stopped until the window turns.
                </jh-notification>`:f}
            ${t.spend?.refusing&&!t.spend.unmetered?u`<jh-notification appearance="neutral" type="banner" hide-dismiss-button>
                  The meter is pacing work: the factory is idle because it is over budget, not because
                  there is nothing to do.
                </jh-notification>`:f}`})}
      </jh-card>
      <jh-card header-title="In flight" header-subtitle="What the factory is doing right now, unit by unit" padding="medium">
        ${this._running.render({pending:()=>u`<jh-progress accessible-label="Loading in-flight work"></jh-progress>`,error:t=>u`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>(t?.length??0)===0?u`<p class="idle">Nothing in flight.</p>`:u`<jh-list-group accessible-label="In-flight work">
                ${t.map(e=>u`
                  <jh-list-item
                    primary-text=${e.task||e.id}
                    secondary-text=${e.note||Pr(e)}
                    primary-metadata=${e.stage??""}
                    secondary-metadata=${Je(e.in_state_sec)+" in stage"}
                    show-divider>
                    <jh-tag slot="jh-list-item-left" size="small" label=${e.state}></jh-tag>
                    <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
                      label="Open" href=${"/items/"+e.id}></jh-button>
                  </jh-list-item>`)}
              </jh-list-group>`})}
      </jh-card>
      <jh-card header-title="Activity" header-subtitle="Everything the factory does, as it does it" padding="medium">
        <div class="live">${this._activity.connected?"live":"reconnecting\u2026"}</div>
        <div class="feed">
          <jh-list-group accessible-label="Activity feed">
            ${this._activity.events.slice().reverse().map(t=>u`
              <jh-list-item
                primary-text=${t.subject}
                secondary-text=${t.scope}
                primary-metadata=${new Date(t.at).toLocaleTimeString()}
                show-divider>
                <jh-tag slot="jh-list-item-left" size="small" label=${t.kind}></jh-tag>
              </jh-list-item>`)}
          </jh-list-group>
        </div>
      </jh-card>
    `}};customElements.define("box-dashboard",eo);var oo=class extends _{static styles=S`
    :host { display: block; }
    .filters { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
    .filters button {
      font: inherit; font-size: .8rem; cursor: pointer; padding: 4px 10px; border-radius: 999px;
      border: 1px solid var(--jh-color-border-primary-enabled, #ccc);
      background: transparent; color: var(--jh-color-content-primary-enabled);
    }
    .filters button[aria-pressed="true"] {
      background: var(--jh-color-container-secondary-enabled); font-weight: 600;
    }
    .note { white-space: pre-wrap; font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
    .blocked { font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
    .empty { color: var(--jh-color-content-secondary-enabled); }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
  `;static properties={_filter:{state:!0}};constructor(){super(),this._filter="live",this._activity=new it(this,{limit:1,onEvent:()=>this._refresh()})}_task=new z(this,{task:async([t],{signal:e})=>ne(to[t],e),args:()=>[this._filter]});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>this._task.run(),400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}render(){return u`
      <jh-card header-title="Work" header-subtitle="Every unit, and why it is where it is" padding="medium">
        <div class="filters" role="group" aria-label="Filter by state">
          ${Object.keys(to).map(t=>u`
            <button type="button" aria-pressed=${this._filter===t?"true":"false"}
              @click=${()=>{this._filter=t}}>${t}</button>`)}
        </div>
        <div class="live">${this._activity.connected?"live":"reconnecting\u2026"}</div>
        ${this._task.render({pending:()=>u`<jh-progress accessible-label="Loading work"></jh-progress>`,error:t=>u`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>(t?.length??0)===0?u`<p class="empty">Nothing in this state.</p>`:u`<jh-list-group accessible-label="Work items">
                ${t.map(e=>u`
                  <jh-list-item
                    primary-text=${e.task||e.id}
                    secondary-text=${Pr(e)}
                    primary-metadata=${Je(e.age_sec)}
                    secondary-metadata=${e.stage?`${e.stage} \xB7 ${Je(e.in_state_sec)}`:""}
                    show-divider>
                    <jh-tag slot="jh-list-item-left" size="small" label=${e.state}></jh-tag>
                    <div slot="jh-list-item-content">
                      ${e.note?u`<div class="note">${e.note}</div>`:f}
                      ${e.blocked_by?.length?u`<div class="blocked">blocked by ${e.blocked_by.join(", ")}</div>`:f}
                    </div>
                    <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
                      label="Open" href=${"/items/"+e.id}></jh-button>
                  </jh-list-item>`)}
              </jh-list-group>`})}
      </jh-card>`}};customElements.define("box-work",oo);var ro=class extends _{static styles=S`:host { display: block; } .empty { color: var(--jh-color-content-secondary-enabled); }`;_task=new z(this,{task:async(t,{signal:e})=>Oo(e),args:()=>[]});render(){return u`
      <jh-card header-title="Exception queue" header-subtitle="Parked work waiting on a person" padding="medium">
        ${this._task.render({pending:()=>u`<jh-progress accessible-label="Loading exceptions"></jh-progress>`,error:t=>u`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>(t?.length??0)===0?u`<p class="empty">Nothing here — the factory has nothing waiting on a person.</p>`:u`<jh-list-group accessible-label="Parked items">
                ${t.map(e=>u`
                  <jh-list-item primary-text=${e.id} secondary-text=${e.note} show-divider>
                    <jh-tag slot="jh-list-item-left" size="small" label=${e.state}></jh-tag>
                    <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
                      label="Open" href=${"/items/"+e.id}></jh-button>
                  </jh-list-item>`)}
              </jh-list-group>`})}
      </jh-card>`}};customElements.define("box-exceptions",ro);var io=class extends _{static properties={itemId:{attribute:!1}};static styles=S`
    :host { display: grid; gap: 16px; }
    pre { white-space: pre-wrap; word-break: break-all; font-size: .8rem;
          background: var(--jh-color-container-secondary-enabled); padding: 8px; border-radius: 6px; }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
  `;constructor(){super(),this._activity=new it(this,{limit:1,onEvent:t=>{t.scope===`workitem/${this.itemId}`&&this._refresh()}})}_task=new z(this,{task:async([t],{signal:e})=>{let[o,r]=await Promise.all([No(t,e),Ho(t,e)]);return{item:o,evidence:r}},args:()=>[this.itemId]});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>this._task.run(),400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}render(){return this._task.render({pending:()=>u`<jh-progress accessible-label="Loading item"></jh-progress>`,error:t=>u`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:({item:t,evidence:e})=>u`
        <jh-card header-title=${t.id} header-subtitle=${t.task} padding="medium">
          <p><jh-tag size="small" label=${t.state}></jh-tag>
             ${t.head?u` <a href=${"/provenance/"+t.head}><code>${t.head.slice(0,12)}</code></a>`:f}</p>
          ${t.note?u`<pre>${t.note}</pre>`:f}
        </jh-card>
        <jh-card header-title="Timeline" header-subtitle="What this run did, in order" padding="medium">
          <div class="live">${this._activity.connected?"live":"reconnecting\u2026"}</div>
          <jh-list-group accessible-label="Run timeline">
            ${(e??[]).map(o=>u`
              <jh-list-item
                primary-text=${o.Kind}
                secondary-text=${di(o)}
                primary-metadata=${o.At?new Date(o.At).toLocaleTimeString():""}
                secondary-metadata=${o.Actor}
                show-divider></jh-list-item>`)}
          </jh-list-group>
        </jh-card>`})}};customElements.define("box-item",io);var so=class extends _{static properties={sha:{attribute:!1}};static styles=S`:host { display: block; }`;_task=new z(this,{task:async([t],{signal:e})=>t?Do(t,e):[],args:()=>[this.sha]});render(){return u`
      <jh-card header-title="Provenance" header-subtitle="Everything recorded about one commit" padding="medium">
        ${this._task.render({pending:()=>u`<jh-progress accessible-label="Loading provenance"></jh-progress>`,error:t=>u`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>(t?.length??0)===0?u`<p>Nothing recorded about <code>${this.sha}</code>.</p>`:u`<jh-list-group accessible-label="Provenance records">
                ${t.map(e=>u`
                  <jh-list-item primary-text=${e.Kind} secondary-text=${e.Scope}
                    primary-metadata=${"seq "+e.Seq} secondary-metadata=${e.Actor} show-divider></jh-list-item>`)}
              </jh-list-group>`})}
      </jh-card>`}};customElements.define("box-provenance",so);var ao=class extends _{static styles=S`
    :host { display: block; }
    form { display: grid; gap: 16px; max-width: 560px; }
    form .actions { padding-top: 8px; }
  `;static properties={_routes:{state:!0},_route:{state:!0},_key:{state:!0},_payload:{state:!0},_result:{state:!0},_error:{state:!0},_pending:{state:!0}};constructor(){super(),this._routes=[],this._route="",this._key="",this._payload="",this._result=null,this._error="",this._pending=!1}connectedCallback(){super.connectedCallback(),ae().then(t=>{this._routes=t.routes,!this._route&&t.routes.length&&(this._route=t.routes[0])}).catch(t=>{this._error=t.message})}async _submit(t){t.preventDefault(),this._pending=!0,this._error="",this._result=null;try{this._result=await Io(this._route,this._key,this._payload)}catch(e){this._error=e.message}finally{this._pending=!1}}render(){return u`
      <jh-card header-title="Submit work" header-subtitle="Open an ingress; the daemon's next tick takes it from there" padding="medium">
        <form @submit=${this._submit}>
          <jh-radio-group label="Route" name="route" .value=${this._route}
            helper-text="Routes are the operator's configuration; work inherits the route's target and authority"
            @jh-change=${t=>{this._route=t.target.value}}>
            ${this._routes.map(t=>u`<jh-radio label=${t} value=${t} ?checked=${t===this._route}></jh-radio>`)}
          </jh-radio-group>
          <jh-input label="Occurrence key" required
            helper-text="The same key always names the same work — redelivery is safe"
            .value=${this._key}
            @jh-change=${t=>{this._key=t.target.value}}
            @input=${t=>{this._key=t.composedPath()[0].value??this._key}}></jh-input>
          <jh-input type="textarea" label="Task" rows="3" auto-grow required
            helper-text="What should be done, in plain words"
            .value=${this._payload}
            @jh-change=${t=>{this._payload=t.target.value}}
            @input=${t=>{this._payload=t.composedPath()[0].value??this._payload}}></jh-input>
          <div class="actions">
            <jh-button submit appearance="primary" label="Open ingress"
              ?pending=${this._pending} ?disabled=${!this._route||!this._key||!this._payload}></jh-button>
          </div>
        </form>
        ${this._error?u`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${this._error}</jh-notification>`:f}
        ${this._result?u`
          <jh-notification appearance="positive" type="banner" hide-dismiss-button>
            ${this._result.opened?"Opened":"Already open"} — item
            <a href=${"/items/"+this._result.item}>${this._result.item}</a>
          </jh-notification>`:f}
      </jh-card>`}};customElements.define("box-submit",ao);Uo();var no=class extends _{static styles=S`
    :host { display: block; max-width: 960px; margin: 0 auto; padding: 16px; }
    header { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; margin-bottom: 16px; }
    h1 { font-size: 1.2rem; margin: 0; }
    nav { display: flex; gap: 4px; flex: 1; }
    nav a { text-decoration: none; padding: 6px 10px; border-radius: 6px;
            color: var(--jh-color-content-primary-enabled); font-size: .9rem; }
    nav a.active { background: var(--jh-color-container-secondary-enabled); font-weight: 600; }
    .controls { display: flex; gap: 8px; align-items: end; }
    main { display: block; }
  `;static properties={_reason:{state:!0},_controlError:{state:!0}};constructor(){super(),this._reason="",this._controlError=""}_router=new ut(this,[{path:"/",render:()=>u`<box-dashboard></box-dashboard>`},{path:"/work",render:()=>u`<box-work></box-work>`},{path:"/exceptions",render:()=>u`<box-exceptions></box-exceptions>`},{path:"/items/:id",render:({id:t})=>u`<box-item .itemId=${t}></box-item>`},{path:"/provenance/:sha",render:({sha:t})=>u`<box-provenance .sha=${t}></box-provenance>`},{path:"/submit",render:()=>u`<box-submit></box-submit>`}],{fallback:{render:()=>u`<jh-notification appearance="neutral" type="banner" hide-dismiss-button>No such page.</jh-notification>`}});_active(t){return location.pathname===t?"active":""}async _control(t){this._controlError="";try{await(t==="stop"?Bo(this._reason):qo(this._reason)),this._reason="",this.requestUpdate()}catch(e){this._controlError=e.message}}render(){return u`
      <header>
        <h1>Box Control Room</h1>
        <nav>
          <a href="/" class=${this._active("/")}>Dashboard</a>
          <a href="/work" class=${this._active("/work")}>Work</a>
          <a href="/exceptions" class=${this._active("/exceptions")}>Exceptions</a>
          <a href="/submit" class=${this._active("/submit")}>Submit</a>
        </nav>
        <div class="controls">
          <jh-input size="small" label="Reason" .value=${this._reason}
            @jh-change=${t=>{this._reason=t.target.value}}></jh-input>
          <jh-button appearance="danger" size="small" label="Stop"
            ?disabled=${!this._reason} @click=${()=>this._control("stop")}></jh-button>
          <jh-button appearance="secondary" size="small" label="Resume"
            ?disabled=${!this._reason} @click=${()=>this._control("resume")}></jh-button>
        </div>
      </header>
      ${this._controlError?u`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${this._controlError}</jh-notification>`:""}
      <main>${this._router.outlet()}</main>
    `}};customElements.define("box-app",no);export{no as BoxApp};
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

@lit/task/task.js:
  (**
   * @license
   * Copyright 2017 Google LLC
   * SPDX-License-Identifier: BSD-3-Clause
   *)
*/
