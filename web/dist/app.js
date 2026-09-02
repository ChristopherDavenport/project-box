var St=globalThis,Et=St.ShadowRoot&&(St.ShadyCSS===void 0||St.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,Zt=Symbol(),fo=new WeakMap,at=class{constructor(t,e,o){if(this._$cssResult$=!0,o!==Zt)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=t,this.t=e}get styleSheet(){let t=this.o,e=this.t;if(Et&&t===void 0){let o=e!==void 0&&e.length===1;o&&(t=fo.get(e)),t===void 0&&((this.o=t=new CSSStyleSheet).replaceSync(this.cssText),o&&fo.set(e,t))}return t}toString(){return this.cssText}},yo=i=>new at(typeof i=="string"?i:i+"",void 0,Zt),A=(i,...t)=>{let e=i.length===1?i[0]:t.reduce((o,r,s)=>o+(a=>{if(a._$cssResult$===!0)return a.cssText;if(typeof a=="number")return a;throw Error("Value passed to 'css' function must be a 'css' function result: "+a+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(r)+i[s+1],i[0]);return new at(e,i,Zt)},jo=(i,t)=>{if(Et)i.adoptedStyleSheets=t.map(e=>e instanceof CSSStyleSheet?e:e.styleSheet);else for(let e of t){let o=document.createElement("style"),r=St.litNonce;r!==void 0&&o.setAttribute("nonce",r),o.textContent=e.cssText,i.appendChild(o)}},Gt=Et?i=>i:i=>i instanceof CSSStyleSheet?(t=>{let e="";for(let o of t.cssRules)e+=o.cssText;return yo(e)})(i):i;var{is:Mr,defineProperty:Ir,getOwnPropertyDescriptor:Br,getOwnPropertyNames:qr,getOwnPropertySymbols:Vr,getPrototypeOf:Wr}=Object,zt=globalThis,$o=zt.trustedTypes,Kr=$o?$o.emptyScript:"",Fr=zt.reactiveElementPolyfillSupport,nt=(i,t)=>i,Yt={toAttribute(i,t){switch(t){case Boolean:i=i?Kr:null;break;case Object:case Array:i=i==null?i:JSON.stringify(i)}return i},fromAttribute(i,t){let e=i;switch(t){case Boolean:e=i!==null;break;case Number:e=i===null?null:Number(i);break;case Object:case Array:try{e=JSON.parse(i)}catch{e=null}}return e}},Ct=(i,t)=>!Mr(i,t),xo={attribute:!0,type:String,converter:Yt,reflect:!1,useDefault:!1,hasChanged:Ct};Symbol.metadata??=Symbol("metadata"),zt.litPropertyMetadata??=new WeakMap;var T=class extends HTMLElement{static addInitializer(t){this._$Ei(),(this.l??=[]).push(t)}static get observedAttributes(){return this.finalize(),this._$Eh&&[...this._$Eh.keys()]}static createProperty(t,e=xo){if(e.state&&(e.attribute=!1),this._$Ei(),this.prototype.hasOwnProperty(t)&&((e=Object.create(e)).wrapped=!0),this.elementProperties.set(t,e),!e.noAccessor){let o=Symbol(),r=this.getPropertyDescriptor(t,o,e);r!==void 0&&Ir(this.prototype,t,r)}}static getPropertyDescriptor(t,e,o){let{get:r,set:s}=Br(this.prototype,t)??{get(){return this[e]},set(a){this[e]=a}};return{get:r,set(a){let h=r?.call(this);s?.call(this,a),this.requestUpdate(t,h,o)},configurable:!0,enumerable:!0}}static getPropertyOptions(t){return this.elementProperties.get(t)??xo}static _$Ei(){if(this.hasOwnProperty(nt("elementProperties")))return;let t=Wr(this);t.finalize(),t.l!==void 0&&(this.l=[...t.l]),this.elementProperties=new Map(t.elementProperties)}static finalize(){if(this.hasOwnProperty(nt("finalized")))return;if(this.finalized=!0,this._$Ei(),this.hasOwnProperty(nt("properties"))){let e=this.properties,o=[...qr(e),...Vr(e)];for(let r of o)this.createProperty(r,e[r])}let t=this[Symbol.metadata];if(t!==null){let e=litPropertyMetadata.get(t);if(e!==void 0)for(let[o,r]of e)this.elementProperties.set(o,r)}this._$Eh=new Map;for(let[e,o]of this.elementProperties){let r=this._$Eu(e,o);r!==void 0&&this._$Eh.set(r,e)}this.elementStyles=this.finalizeStyles(this.styles)}static finalizeStyles(t){let e=[];if(Array.isArray(t)){let o=new Set(t.flat(1/0).reverse());for(let r of o)e.unshift(Gt(r))}else t!==void 0&&e.push(Gt(t));return e}static _$Eu(t,e){let o=e.attribute;return o===!1?void 0:typeof o=="string"?o:typeof t=="string"?t.toLowerCase():void 0}constructor(){super(),this._$Ep=void 0,this.isUpdatePending=!1,this.hasUpdated=!1,this._$Em=null,this._$Ev()}_$Ev(){this._$ES=new Promise(t=>this.enableUpdating=t),this._$AL=new Map,this._$E_(),this.requestUpdate(),this.constructor.l?.forEach(t=>t(this))}addController(t){(this._$EO??=new Set).add(t),this.renderRoot!==void 0&&this.isConnected&&t.hostConnected?.()}removeController(t){this._$EO?.delete(t)}_$E_(){let t=new Map,e=this.constructor.elementProperties;for(let o of e.keys())this.hasOwnProperty(o)&&(t.set(o,this[o]),delete this[o]);t.size>0&&(this._$Ep=t)}createRenderRoot(){let t=this.shadowRoot??this.attachShadow(this.constructor.shadowRootOptions);return jo(t,this.constructor.elementStyles),t}connectedCallback(){this.renderRoot??=this.createRenderRoot(),this.enableUpdating(!0),this._$EO?.forEach(t=>t.hostConnected?.())}enableUpdating(t){}disconnectedCallback(){this._$EO?.forEach(t=>t.hostDisconnected?.())}attributeChangedCallback(t,e,o){this._$AK(t,o)}_$ET(t,e){let o=this.constructor.elementProperties.get(t),r=this.constructor._$Eu(t,o);if(r!==void 0&&o.reflect===!0){let s=(o.converter?.toAttribute!==void 0?o.converter:Yt).toAttribute(e,o.type);this._$Em=t,s==null?this.removeAttribute(r):this.setAttribute(r,s),this._$Em=null}}_$AK(t,e){let o=this.constructor,r=o._$Eh.get(t);if(r!==void 0&&this._$Em!==r){let s=o.getPropertyOptions(r),a=typeof s.converter=="function"?{fromAttribute:s.converter}:s.converter?.fromAttribute!==void 0?s.converter:Yt;this._$Em=r;let h=a.fromAttribute(e,s.type);this[r]=h??this._$Ej?.get(r)??h,this._$Em=null}}requestUpdate(t,e,o,r=!1,s){if(t!==void 0){let a=this.constructor;if(r===!1&&(s=this[t]),o??=a.getPropertyOptions(t),!((o.hasChanged??Ct)(s,e)||o.useDefault&&o.reflect&&s===this._$Ej?.get(t)&&!this.hasAttribute(a._$Eu(t,o))))return;this.C(t,e,o)}this.isUpdatePending===!1&&(this._$ES=this._$EP())}C(t,e,{useDefault:o,reflect:r,wrapped:s},a){o&&!(this._$Ej??=new Map).has(t)&&(this._$Ej.set(t,a??e??this[t]),s!==!0||a!==void 0)||(this._$AL.has(t)||(this.hasUpdated||o||(e=void 0),this._$AL.set(t,e)),r===!0&&this._$Em!==t&&(this._$Eq??=new Set).add(t))}async _$EP(){this.isUpdatePending=!0;try{await this._$ES}catch(e){Promise.reject(e)}let t=this.scheduleUpdate();return t!=null&&await t,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){if(!this.isUpdatePending)return;if(!this.hasUpdated){if(this.renderRoot??=this.createRenderRoot(),this._$Ep){for(let[r,s]of this._$Ep)this[r]=s;this._$Ep=void 0}let o=this.constructor.elementProperties;if(o.size>0)for(let[r,s]of o){let{wrapped:a}=s,h=this[r];a!==!0||this._$AL.has(r)||h===void 0||this.C(r,void 0,s,h)}}let t=!1,e=this._$AL;try{t=this.shouldUpdate(e),t?(this.willUpdate(e),this._$EO?.forEach(o=>o.hostUpdate?.()),this.update(e)):this._$EM()}catch(o){throw t=!1,this._$EM(),o}t&&this._$AE(e)}willUpdate(t){}_$AE(t){this._$EO?.forEach(e=>e.hostUpdated?.()),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(t)),this.updated(t)}_$EM(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$ES}shouldUpdate(t){return!0}update(t){this._$Eq&&=this._$Eq.forEach(e=>this._$ET(e,this[e])),this._$EM()}updated(t){}firstUpdated(t){}};T.elementStyles=[],T.shadowRootOptions={mode:"open"},T[nt("elementProperties")]=new Map,T[nt("finalized")]=new Map,Fr?.({ReactiveElement:T}),(zt.reactiveElementVersions??=[]).push("2.1.2");var re=globalThis,_o=i=>i,Tt=re.trustedTypes,wo=Tt?Tt.createPolicy("lit-html",{createHTML:i=>i}):void 0,Co="$lit$",R=`lit$${Math.random().toFixed(9).slice(2)}$`,To="?"+R,Zr=`<${To}>`,M=document,ht=()=>M.createComment(""),ct=i=>i===null||typeof i!="object"&&typeof i!="function",ie=Array.isArray,Gr=i=>ie(i)||typeof i?.[Symbol.iterator]=="function",Qt=`[ 	
\f\r]`,lt=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,ko=/-->/g,Ao=/>/g,H=RegExp(`>|${Qt}(?:([^\\s"'>=/]+)(${Qt}*=${Qt}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),So=/'/g,Eo=/"/g,Lo=/^(?:script|style|textarea|title)$/i,se=i=>(t,...e)=>({_$litType$:i,strings:t,values:e}),c=se(1),Si=se(2),Ei=se(3),I=Symbol.for("lit-noChange"),g=Symbol.for("lit-nothing"),zo=new WeakMap,D=M.createTreeWalker(M,129);function Po(i,t){if(!ie(i)||!i.hasOwnProperty("raw"))throw Error("invalid template strings array");return wo!==void 0?wo.createHTML(t):t}var Yr=(i,t)=>{let e=i.length-1,o=[],r,s=t===2?"<svg>":t===3?"<math>":"",a=lt;for(let h=0;h<e;h++){let n=i[h],l,u,p=-1,v=0;for(;v<n.length&&(a.lastIndex=v,u=a.exec(n),u!==null);)v=a.lastIndex,a===lt?u[1]==="!--"?a=ko:u[1]!==void 0?a=Ao:u[2]!==void 0?(Lo.test(u[2])&&(r=RegExp("</"+u[2],"g")),a=H):u[3]!==void 0&&(a=H):a===H?u[0]===">"?(a=r??lt,p=-1):u[1]===void 0?p=-2:(p=a.lastIndex-u[2].length,l=u[1],a=u[3]===void 0?H:u[3]==='"'?Eo:So):a===Eo||a===So?a=H:a===ko||a===Ao?a=lt:(a=H,r=void 0);let m=a===H&&i[h+1].startsWith("/>")?" ":"";s+=a===lt?n+Zr:p>=0?(o.push(l),n.slice(0,p)+Co+n.slice(p)+R+m):n+R+(p===-2?h:m)}return[Po(i,s+(i[e]||"<?>")+(t===2?"</svg>":t===3?"</math>":"")),o]},dt=class i{constructor({strings:t,_$litType$:e},o){let r;this.parts=[];let s=0,a=0,h=t.length-1,n=this.parts,[l,u]=Yr(t,e);if(this.el=i.createElement(l,o),D.currentNode=this.el.content,e===2||e===3){let p=this.el.content.firstChild;p.replaceWith(...p.childNodes)}for(;(r=D.nextNode())!==null&&n.length<h;){if(r.nodeType===1){if(r.hasAttributes())for(let p of r.getAttributeNames())if(p.endsWith(Co)){let v=u[a++],m=r.getAttribute(p).split(R),k=/([.?@])?(.*)/.exec(v);n.push({type:1,index:s,name:k[2],strings:m,ctor:k[1]==="."?Jt:k[1]==="?"?te:k[1]==="@"?ee:X}),r.removeAttribute(p)}else p.startsWith(R)&&(n.push({type:6,index:s}),r.removeAttribute(p));if(Lo.test(r.tagName)){let p=r.textContent.split(R),v=p.length-1;if(v>0){r.textContent=Tt?Tt.emptyScript:"";for(let m=0;m<v;m++)r.append(p[m],ht()),D.nextNode(),n.push({type:2,index:++s});r.append(p[v],ht())}}}else if(r.nodeType===8)if(r.data===To)n.push({type:2,index:s});else{let p=-1;for(;(p=r.data.indexOf(R,p+1))!==-1;)n.push({type:7,index:s}),p+=R.length-1}s++}}static createElement(t,e){let o=M.createElement("template");return o.innerHTML=t,o}};function Q(i,t,e=i,o){if(t===I)return t;let r=o!==void 0?e._$Co?.[o]:e._$Cl,s=ct(t)?void 0:t._$litDirective$;return r?.constructor!==s&&(r?._$AO?.(!1),s===void 0?r=void 0:(r=new s(i),r._$AT(i,e,o)),o!==void 0?(e._$Co??=[])[o]=r:e._$Cl=r),r!==void 0&&(t=Q(i,r._$AS(i,t.values),r,o)),t}var Xt=class{constructor(t,e){this._$AV=[],this._$AN=void 0,this._$AD=t,this._$AM=e}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(t){let{el:{content:e},parts:o}=this._$AD,r=(t?.creationScope??M).importNode(e,!0);D.currentNode=r;let s=D.nextNode(),a=0,h=0,n=o[0];for(;n!==void 0;){if(a===n.index){let l;n.type===2?l=new pt(s,s.nextSibling,this,t):n.type===1?l=new n.ctor(s,n.name,n.strings,this,t):n.type===6&&(l=new oe(s,this,t)),this._$AV.push(l),n=o[++h]}a!==n?.index&&(s=D.nextNode(),a++)}return D.currentNode=M,r}p(t){let e=0;for(let o of this._$AV)o!==void 0&&(o.strings!==void 0?(o._$AI(t,o,e),e+=o.strings.length-2):o._$AI(t[e])),e++}},pt=class i{get _$AU(){return this._$AM?._$AU??this._$Cv}constructor(t,e,o,r){this.type=2,this._$AH=g,this._$AN=void 0,this._$AA=t,this._$AB=e,this._$AM=o,this.options=r,this._$Cv=r?.isConnected??!0}get parentNode(){let t=this._$AA.parentNode,e=this._$AM;return e!==void 0&&t?.nodeType===11&&(t=e.parentNode),t}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(t,e=this){t=Q(this,t,e),ct(t)?t===g||t==null||t===""?(this._$AH!==g&&this._$AR(),this._$AH=g):t!==this._$AH&&t!==I&&this._(t):t._$litType$!==void 0?this.$(t):t.nodeType!==void 0?this.T(t):Gr(t)?this.k(t):this._(t)}O(t){return this._$AA.parentNode.insertBefore(t,this._$AB)}T(t){this._$AH!==t&&(this._$AR(),this._$AH=this.O(t))}_(t){this._$AH!==g&&ct(this._$AH)?this._$AA.nextSibling.data=t:this.T(M.createTextNode(t)),this._$AH=t}$(t){let{values:e,_$litType$:o}=t,r=typeof o=="number"?this._$AC(t):(o.el===void 0&&(o.el=dt.createElement(Po(o.h,o.h[0]),this.options)),o);if(this._$AH?._$AD===r)this._$AH.p(e);else{let s=new Xt(r,this),a=s.u(this.options);s.p(e),this.T(a),this._$AH=s}}_$AC(t){let e=zo.get(t.strings);return e===void 0&&zo.set(t.strings,e=new dt(t)),e}k(t){ie(this._$AH)||(this._$AH=[],this._$AR());let e=this._$AH,o,r=0;for(let s of t)r===e.length?e.push(o=new i(this.O(ht()),this.O(ht()),this,this.options)):o=e[r],o._$AI(s),r++;r<e.length&&(this._$AR(o&&o._$AB.nextSibling,r),e.length=r)}_$AR(t=this._$AA.nextSibling,e){for(this._$AP?.(!1,!0,e);t!==this._$AB;){let o=_o(t).nextSibling;_o(t).remove(),t=o}}setConnected(t){this._$AM===void 0&&(this._$Cv=t,this._$AP?.(t))}},X=class{get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}constructor(t,e,o,r,s){this.type=1,this._$AH=g,this._$AN=void 0,this.element=t,this.name=e,this._$AM=r,this.options=s,o.length>2||o[0]!==""||o[1]!==""?(this._$AH=Array(o.length-1).fill(new String),this.strings=o):this._$AH=g}_$AI(t,e=this,o,r){let s=this.strings,a=!1;if(s===void 0)t=Q(this,t,e,0),a=!ct(t)||t!==this._$AH&&t!==I,a&&(this._$AH=t);else{let h=t,n,l;for(t=s[0],n=0;n<s.length-1;n++)l=Q(this,h[o+n],e,n),l===I&&(l=this._$AH[n]),a||=!ct(l)||l!==this._$AH[n],l===g?t=g:t!==g&&(t+=(l??"")+s[n+1]),this._$AH[n]=l}a&&!r&&this.j(t)}j(t){t===g?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,t??"")}},Jt=class extends X{constructor(){super(...arguments),this.type=3}j(t){this.element[this.name]=t===g?void 0:t}},te=class extends X{constructor(){super(...arguments),this.type=4}j(t){this.element.toggleAttribute(this.name,!!t&&t!==g)}},ee=class extends X{constructor(t,e,o,r,s){super(t,e,o,r,s),this.type=5}_$AI(t,e=this){if((t=Q(this,t,e,0)??g)===I)return;let o=this._$AH,r=t===g&&o!==g||t.capture!==o.capture||t.once!==o.once||t.passive!==o.passive,s=t!==g&&(o===g||r);r&&this.element.removeEventListener(this.name,this,o),s&&this.element.addEventListener(this.name,this,t),this._$AH=t}handleEvent(t){typeof this._$AH=="function"?this._$AH.call(this.options?.host??this.element,t):this._$AH.handleEvent(t)}},oe=class{constructor(t,e,o){this.element=t,this.type=6,this._$AN=void 0,this._$AM=e,this.options=o}get _$AU(){return this._$AM._$AU}_$AI(t){Q(this,t)}};var Qr=re.litHtmlPolyfillSupport;Qr?.(dt,pt),(re.litHtmlVersions??=[]).push("3.3.3");var Ro=(i,t,e)=>{let o=e?.renderBefore??t,r=o._$litPart$;if(r===void 0){let s=e?.renderBefore??null;o._$litPart$=r=new pt(t.insertBefore(ht(),s),s,void 0,e??{})}return r._$AI(i),r};var ae=globalThis,x=class extends T{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){let t=super.createRenderRoot();return this.renderOptions.renderBefore??=t.firstChild,t}update(t){let e=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(t),this._$Do=Ro(e,this.renderRoot,this.renderOptions)}connectedCallback(){super.connectedCallback(),this._$Do?.setConnected(!0)}disconnectedCallback(){super.disconnectedCallback(),this._$Do?.setConnected(!1)}render(){return I}};x._$litElement$=!0,x.finalized=!0,ae.litElementHydrateSupport?.({LitElement:x});var Xr=ae.litElementPolyfillSupport;Xr?.({LitElement:x});(ae.litElementVersions??=[]).push("4.2.2");var Uo=new WeakMap,Oo=i=>{if((e=>e.pattern!==void 0)(i))return i.pattern;let t=Uo.get(i);return t===void 0&&Uo.set(i,t=new URLPattern({pathname:i.path})),t},ut=class{constructor(t,e,o){this.routes=[],this.o=[],this.t={},this.i=r=>{if(r.routes===this)return;let s=r.routes;this.o.push(s),s.h=this,r.stopImmediatePropagation(),r.onDisconnect=()=>{this.o?.splice(this.o.indexOf(s)>>>0,1)};let a=No(this.t);a!==void 0&&s.goto(a)},(this.l=t).addController(this),this.routes=[...e],this.fallback=o?.fallback}link(t){if(t?.startsWith("/"))return t;if(t?.startsWith("."))throw Error("Not implemented");return t??=this.u,(this.h?.link()??"")+t}async goto(t){let e;if(this.routes.length===0&&this.fallback===void 0)e=t,this.u="",this.t={0:e};else{let o=this.p(t);if(o===void 0)throw Error("No route found for "+t);let r=Oo(o).exec({pathname:t}),s=r?.pathname.groups??{};if(e=No(s),typeof o.enter=="function"&&await o.enter(s)===!1)return;this.v=o,this.t=s,this.u=e===void 0?t:t.substring(0,t.length-e.length)}if(e!==void 0)for(let o of this.o)o.goto(e);this.l.requestUpdate()}outlet(){return this.v?.render?.(this.t)}get params(){return this.t}p(t){let e=this.routes.find(o=>Oo(o).test({pathname:t}));return e||this.fallback===void 0?e:this.fallback?{...this.fallback,path:"/*"}:void 0}hostConnected(){this.l.addEventListener(J.eventName,this.i);let t=new J(this);this.l.dispatchEvent(t),this._=t.onDisconnect}hostDisconnected(){this._?.(),this.h=void 0}},No=i=>{let t;for(let e of Object.keys(i))/\d+/.test(e)&&(t===void 0||e>t)&&(t=e);return t&&i[t]},J=class i extends Event{constructor(t){super(i.eventName,{bubbles:!0,composed:!0,cancelable:!1}),this.routes=t}};J.eventName="lit-routes-connected";var Jr=location.origin||location.protocol+"//"+location.host,bt=class extends ut{constructor(){super(...arguments),this.m=t=>{let e=t.button!==0||t.metaKey||t.ctrlKey||t.shiftKey;if(t.defaultPrevented||e)return;let o=t.composedPath().find(a=>a.tagName==="A");if(o===void 0||o.target!==""||o.hasAttribute("download")||o.getAttribute("rel")==="external")return;let r=o.href;if(r===""||r.startsWith("mailto:"))return;let s=window.location;o.origin===Jr&&(t.preventDefault(),r!==s.href&&(window.history.pushState({},"",r),this.goto(o.pathname)))},this.R=t=>{this.goto(window.location.pathname)}}hostConnected(){super.hostConnected(),window.addEventListener("click",this.m),window.addEventListener("popstate",this.R),this.goto(window.location.pathname)}hostDisconnected(){super.hostDisconnected(),window.removeEventListener("click",this.m),window.removeEventListener("popstate",this.R)}};function Ho(){let i=new URL(location.href),t=i.searchParams.get("token");t&&(document.cookie=`box_token=${encodeURIComponent(t)}; path=/; SameSite=Strict`,i.searchParams.delete("token"),history.replaceState({},"",i.toString()))}async function ne(i,t,e){let o=await fetch(t,{method:i,headers:e?{"Content-Type":"application/json"}:void 0,body:e?JSON.stringify(e):void 0,credentials:"same-origin"});if(!o.ok){let r=`${i} ${t}: ${o.status}`;try{r=(await o.json()).error??r}catch{}throw new Error(r)}return o.json()}var le=i=>U("/v1/status",i),he=(i,t)=>U("/v1/items"+(i?.length?`?state=${encodeURIComponent(i.join(","))}`:""),t),Do=i=>U("/v1/exceptions",i),Mo=(i,t)=>U(`/v1/items/${encodeURIComponent(i)}`,t),Io=(i,t)=>U(`/v1/items/${encodeURIComponent(i)}/evidence`,t),Bo=(i,t)=>U(`/v1/provenance/${encodeURIComponent(i)}`,t),qo=i=>U("/v1/activity",i),Vo=i=>U("/v1/sessions",i);var Wo=(i,t,e)=>ne("POST","/v1/ingress",{route:i,key:t,payload:e}),Ko=i=>ne("POST","/v1/stop",{reason:i}),Fo=i=>ne("POST","/v1/resume",{reason:i});async function U(i,t){let e=await fetch(i,{signal:t,credentials:"same-origin"});if(!e.ok)throw new Error(`${i}: ${e.status}`);return e.json()}var Lt=window,Pt=Lt.ShadowRoot&&(Lt.ShadyCSS===void 0||Lt.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,ce=Symbol(),Zo=new WeakMap,vt=class{constructor(t,e,o){if(this._$cssResult$=!0,o!==ce)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=t,this.t=e}get styleSheet(){let t=this.o,e=this.t;if(Pt&&t===void 0){let o=e!==void 0&&e.length===1;o&&(t=Zo.get(e)),t===void 0&&((this.o=t=new CSSStyleSheet).replaceSync(this.cssText),o&&Zo.set(e,t))}return t}toString(){return this.cssText}},Go=i=>new vt(typeof i=="string"?i:i+"",void 0,ce),y=(i,...t)=>{let e=i.length===1?i[0]:t.reduce((o,r,s)=>o+(a=>{if(a._$cssResult$===!0)return a.cssText;if(typeof a=="number")return a;throw Error("Value passed to 'css' function must be a 'css' function result: "+a+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(r)+i[s+1],i[0]);return new vt(e,i,ce)},de=(i,t)=>{Pt?i.adoptedStyleSheets=t.map(e=>e instanceof CSSStyleSheet?e:e.styleSheet):t.forEach(e=>{let o=document.createElement("style"),r=Lt.litNonce;r!==void 0&&o.setAttribute("nonce",r),o.textContent=e.cssText,i.appendChild(o)})},Rt=Pt?i=>i:i=>i instanceof CSSStyleSheet?(t=>{let e="";for(let o of t.cssRules)e+=o.cssText;return Go(e)})(i):i;var pe,Ut=window,Yo=Ut.trustedTypes,ti=Yo?Yo.emptyScript:"",Qo=Ut.reactiveElementPolyfillSupport,be={toAttribute(i,t){switch(t){case Boolean:i=i?ti:null;break;case Object:case Array:i=i==null?i:JSON.stringify(i)}return i},fromAttribute(i,t){let e=i;switch(t){case Boolean:e=i!==null;break;case Number:e=i===null?null:Number(i);break;case Object:case Array:try{e=JSON.parse(i)}catch{e=null}}return e}},Xo=(i,t)=>t!==i&&(t==t||i==i),ue={attribute:!0,type:String,converter:be,reflect:!1,hasChanged:Xo},ve="finalized",L=class extends HTMLElement{constructor(){super(),this._$Ei=new Map,this.isUpdatePending=!1,this.hasUpdated=!1,this._$El=null,this._$Eu()}static addInitializer(t){var e;this.finalize(),((e=this.h)!==null&&e!==void 0?e:this.h=[]).push(t)}static get observedAttributes(){this.finalize();let t=[];return this.elementProperties.forEach((e,o)=>{let r=this._$Ep(o,e);r!==void 0&&(this._$Ev.set(r,o),t.push(r))}),t}static createProperty(t,e=ue){if(e.state&&(e.attribute=!1),this.finalize(),this.elementProperties.set(t,e),!e.noAccessor&&!this.prototype.hasOwnProperty(t)){let o=typeof t=="symbol"?Symbol():"__"+t,r=this.getPropertyDescriptor(t,o,e);r!==void 0&&Object.defineProperty(this.prototype,t,r)}}static getPropertyDescriptor(t,e,o){return{get(){return this[e]},set(r){let s=this[t];this[e]=r,this.requestUpdate(t,s,o)},configurable:!0,enumerable:!0}}static getPropertyOptions(t){return this.elementProperties.get(t)||ue}static finalize(){if(this.hasOwnProperty(ve))return!1;this[ve]=!0;let t=Object.getPrototypeOf(this);if(t.finalize(),t.h!==void 0&&(this.h=[...t.h]),this.elementProperties=new Map(t.elementProperties),this._$Ev=new Map,this.hasOwnProperty("properties")){let e=this.properties,o=[...Object.getOwnPropertyNames(e),...Object.getOwnPropertySymbols(e)];for(let r of o)this.createProperty(r,e[r])}return this.elementStyles=this.finalizeStyles(this.styles),!0}static finalizeStyles(t){let e=[];if(Array.isArray(t)){let o=new Set(t.flat(1/0).reverse());for(let r of o)e.unshift(Rt(r))}else t!==void 0&&e.push(Rt(t));return e}static _$Ep(t,e){let o=e.attribute;return o===!1?void 0:typeof o=="string"?o:typeof t=="string"?t.toLowerCase():void 0}_$Eu(){var t;this._$E_=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$Eg(),this.requestUpdate(),(t=this.constructor.h)===null||t===void 0||t.forEach(e=>e(this))}addController(t){var e,o;((e=this._$ES)!==null&&e!==void 0?e:this._$ES=[]).push(t),this.renderRoot!==void 0&&this.isConnected&&((o=t.hostConnected)===null||o===void 0||o.call(t))}removeController(t){var e;(e=this._$ES)===null||e===void 0||e.splice(this._$ES.indexOf(t)>>>0,1)}_$Eg(){this.constructor.elementProperties.forEach((t,e)=>{this.hasOwnProperty(e)&&(this._$Ei.set(e,this[e]),delete this[e])})}createRenderRoot(){var t;let e=(t=this.shadowRoot)!==null&&t!==void 0?t:this.attachShadow(this.constructor.shadowRootOptions);return de(e,this.constructor.elementStyles),e}connectedCallback(){var t;this.renderRoot===void 0&&(this.renderRoot=this.createRenderRoot()),this.enableUpdating(!0),(t=this._$ES)===null||t===void 0||t.forEach(e=>{var o;return(o=e.hostConnected)===null||o===void 0?void 0:o.call(e)})}enableUpdating(t){}disconnectedCallback(){var t;(t=this._$ES)===null||t===void 0||t.forEach(e=>{var o;return(o=e.hostDisconnected)===null||o===void 0?void 0:o.call(e)})}attributeChangedCallback(t,e,o){this._$AK(t,o)}_$EO(t,e,o=ue){var r;let s=this.constructor._$Ep(t,o);if(s!==void 0&&o.reflect===!0){let a=(((r=o.converter)===null||r===void 0?void 0:r.toAttribute)!==void 0?o.converter:be).toAttribute(e,o.type);this._$El=t,a==null?this.removeAttribute(s):this.setAttribute(s,a),this._$El=null}}_$AK(t,e){var o;let r=this.constructor,s=r._$Ev.get(t);if(s!==void 0&&this._$El!==s){let a=r.getPropertyOptions(s),h=typeof a.converter=="function"?{fromAttribute:a.converter}:((o=a.converter)===null||o===void 0?void 0:o.fromAttribute)!==void 0?a.converter:be;this._$El=s,this[s]=h.fromAttribute(e,a.type),this._$El=null}}requestUpdate(t,e,o){let r=!0;t!==void 0&&(((o=o||this.constructor.getPropertyOptions(t)).hasChanged||Xo)(this[t],e)?(this._$AL.has(t)||this._$AL.set(t,e),o.reflect===!0&&this._$El!==t&&(this._$EC===void 0&&(this._$EC=new Map),this._$EC.set(t,o))):r=!1),!this.isUpdatePending&&r&&(this._$E_=this._$Ej())}async _$Ej(){this.isUpdatePending=!0;try{await this._$E_}catch(e){Promise.reject(e)}let t=this.scheduleUpdate();return t!=null&&await t,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){var t;if(!this.isUpdatePending)return;this.hasUpdated,this._$Ei&&(this._$Ei.forEach((r,s)=>this[s]=r),this._$Ei=void 0);let e=!1,o=this._$AL;try{e=this.shouldUpdate(o),e?(this.willUpdate(o),(t=this._$ES)===null||t===void 0||t.forEach(r=>{var s;return(s=r.hostUpdate)===null||s===void 0?void 0:s.call(r)}),this.update(o)):this._$Ek()}catch(r){throw e=!1,this._$Ek(),r}e&&this._$AE(o)}willUpdate(t){}_$AE(t){var e;(e=this._$ES)===null||e===void 0||e.forEach(o=>{var r;return(r=o.hostUpdated)===null||r===void 0?void 0:r.call(o)}),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(t)),this.updated(t)}_$Ek(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$E_}shouldUpdate(t){return!0}update(t){this._$EC!==void 0&&(this._$EC.forEach((e,o)=>this._$EO(o,this[o],e)),this._$EC=void 0),this._$Ek()}updated(t){}firstUpdated(t){}};L[ve]=!0,L.elementProperties=new Map,L.elementStyles=[],L.shadowRootOptions={mode:"open"},Qo?.({ReactiveElement:L}),((pe=Ut.reactiveElementVersions)!==null&&pe!==void 0?pe:Ut.reactiveElementVersions=[]).push("1.6.3");var ge,Ot=window,tt=Ot.trustedTypes,Jo=tt?tt.createPolicy("lit-html",{createHTML:i=>i}):void 0,fe="$lit$",O=`lit$${(Math.random()+"").slice(9)}$`,ar="?"+O,ei=`<${ar}>`,V=document,mt=()=>V.createComment(""),ft=i=>i===null||typeof i!="object"&&typeof i!="function",nr=Array.isArray,oi=i=>nr(i)||typeof i?.[Symbol.iterator]=="function",me=`[ 	
\f\r]`,gt=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,tr=/-->/g,er=/>/g,B=RegExp(`>|${me}(?:([^\\s"'>=/]+)(${me}*=${me}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),or=/'/g,rr=/"/g,lr=/^(?:script|style|textarea|title)$/i,hr=i=>(t,...e)=>({_$litType$:i,strings:t,values:e}),d=hr(1),Xi=hr(2),W=Symbol.for("lit-noChange"),j=Symbol.for("lit-nothing"),ir=new WeakMap,q=V.createTreeWalker(V,129,null,!1);function cr(i,t){if(!Array.isArray(i)||!i.hasOwnProperty("raw"))throw Error("invalid template strings array");return Jo!==void 0?Jo.createHTML(t):t}var ri=(i,t)=>{let e=i.length-1,o=[],r,s=t===2?"<svg>":"",a=gt;for(let h=0;h<e;h++){let n=i[h],l,u,p=-1,v=0;for(;v<n.length&&(a.lastIndex=v,u=a.exec(n),u!==null);)v=a.lastIndex,a===gt?u[1]==="!--"?a=tr:u[1]!==void 0?a=er:u[2]!==void 0?(lr.test(u[2])&&(r=RegExp("</"+u[2],"g")),a=B):u[3]!==void 0&&(a=B):a===B?u[0]===">"?(a=r??gt,p=-1):u[1]===void 0?p=-2:(p=a.lastIndex-u[2].length,l=u[1],a=u[3]===void 0?B:u[3]==='"'?rr:or):a===rr||a===or?a=B:a===tr||a===er?a=gt:(a=B,r=void 0);let m=a===B&&i[h+1].startsWith("/>")?" ":"";s+=a===gt?n+ei:p>=0?(o.push(l),n.slice(0,p)+fe+n.slice(p)+O+m):n+O+(p===-2?(o.push(void 0),h):m)}return[cr(i,s+(i[e]||"<?>")+(t===2?"</svg>":"")),o]},yt=class i{constructor({strings:t,_$litType$:e},o){let r;this.parts=[];let s=0,a=0,h=t.length-1,n=this.parts,[l,u]=ri(t,e);if(this.el=i.createElement(l,o),q.currentNode=this.el.content,e===2){let p=this.el.content,v=p.firstChild;v.remove(),p.append(...v.childNodes)}for(;(r=q.nextNode())!==null&&n.length<h;){if(r.nodeType===1){if(r.hasAttributes()){let p=[];for(let v of r.getAttributeNames())if(v.endsWith(fe)||v.startsWith(O)){let m=u[a++];if(p.push(v),m!==void 0){let k=r.getAttribute(m.toLowerCase()+fe).split(O),w=/([.?@])?(.*)/.exec(m);n.push({type:1,index:s,name:w[2],strings:k,ctor:w[1]==="."?je:w[1]==="?"?$e:w[1]==="@"?xe:ot})}else n.push({type:6,index:s})}for(let v of p)r.removeAttribute(v)}if(lr.test(r.tagName)){let p=r.textContent.split(O),v=p.length-1;if(v>0){r.textContent=tt?tt.emptyScript:"";for(let m=0;m<v;m++)r.append(p[m],mt()),q.nextNode(),n.push({type:2,index:++s});r.append(p[v],mt())}}}else if(r.nodeType===8)if(r.data===ar)n.push({type:2,index:s});else{let p=-1;for(;(p=r.data.indexOf(O,p+1))!==-1;)n.push({type:7,index:s}),p+=O.length-1}s++}}static createElement(t,e){let o=V.createElement("template");return o.innerHTML=t,o}};function et(i,t,e=i,o){var r,s,a,h;if(t===W)return t;let n=o!==void 0?(r=e._$Co)===null||r===void 0?void 0:r[o]:e._$Cl,l=ft(t)?void 0:t._$litDirective$;return n?.constructor!==l&&((s=n?._$AO)===null||s===void 0||s.call(n,!1),l===void 0?n=void 0:(n=new l(i),n._$AT(i,e,o)),o!==void 0?((a=(h=e)._$Co)!==null&&a!==void 0?a:h._$Co=[])[o]=n:e._$Cl=n),n!==void 0&&(t=et(i,n._$AS(i,t.values),n,o)),t}var ye=class{constructor(t,e){this._$AV=[],this._$AN=void 0,this._$AD=t,this._$AM=e}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(t){var e;let{el:{content:o},parts:r}=this._$AD,s=((e=t?.creationScope)!==null&&e!==void 0?e:V).importNode(o,!0);q.currentNode=s;let a=q.nextNode(),h=0,n=0,l=r[0];for(;l!==void 0;){if(h===l.index){let u;l.type===2?u=new jt(a,a.nextSibling,this,t):l.type===1?u=new l.ctor(a,l.name,l.strings,this,t):l.type===6&&(u=new _e(a,this,t)),this._$AV.push(u),l=r[++n]}h!==l?.index&&(a=q.nextNode(),h++)}return q.currentNode=V,s}v(t){let e=0;for(let o of this._$AV)o!==void 0&&(o.strings!==void 0?(o._$AI(t,o,e),e+=o.strings.length-2):o._$AI(t[e])),e++}},jt=class i{constructor(t,e,o,r){var s;this.type=2,this._$AH=j,this._$AN=void 0,this._$AA=t,this._$AB=e,this._$AM=o,this.options=r,this._$Cp=(s=r?.isConnected)===null||s===void 0||s}get _$AU(){var t,e;return(e=(t=this._$AM)===null||t===void 0?void 0:t._$AU)!==null&&e!==void 0?e:this._$Cp}get parentNode(){let t=this._$AA.parentNode,e=this._$AM;return e!==void 0&&t?.nodeType===11&&(t=e.parentNode),t}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(t,e=this){t=et(this,t,e),ft(t)?t===j||t==null||t===""?(this._$AH!==j&&this._$AR(),this._$AH=j):t!==this._$AH&&t!==W&&this._(t):t._$litType$!==void 0?this.g(t):t.nodeType!==void 0?this.$(t):oi(t)?this.T(t):this._(t)}k(t){return this._$AA.parentNode.insertBefore(t,this._$AB)}$(t){this._$AH!==t&&(this._$AR(),this._$AH=this.k(t))}_(t){this._$AH!==j&&ft(this._$AH)?this._$AA.nextSibling.data=t:this.$(V.createTextNode(t)),this._$AH=t}g(t){var e;let{values:o,_$litType$:r}=t,s=typeof r=="number"?this._$AC(t):(r.el===void 0&&(r.el=yt.createElement(cr(r.h,r.h[0]),this.options)),r);if(((e=this._$AH)===null||e===void 0?void 0:e._$AD)===s)this._$AH.v(o);else{let a=new ye(s,this),h=a.u(this.options);a.v(o),this.$(h),this._$AH=a}}_$AC(t){let e=ir.get(t.strings);return e===void 0&&ir.set(t.strings,e=new yt(t)),e}T(t){nr(this._$AH)||(this._$AH=[],this._$AR());let e=this._$AH,o,r=0;for(let s of t)r===e.length?e.push(o=new i(this.k(mt()),this.k(mt()),this,this.options)):o=e[r],o._$AI(s),r++;r<e.length&&(this._$AR(o&&o._$AB.nextSibling,r),e.length=r)}_$AR(t=this._$AA.nextSibling,e){var o;for((o=this._$AP)===null||o===void 0||o.call(this,!1,!0,e);t&&t!==this._$AB;){let r=t.nextSibling;t.remove(),t=r}}setConnected(t){var e;this._$AM===void 0&&(this._$Cp=t,(e=this._$AP)===null||e===void 0||e.call(this,t))}},ot=class{constructor(t,e,o,r,s){this.type=1,this._$AH=j,this._$AN=void 0,this.element=t,this.name=e,this._$AM=r,this.options=s,o.length>2||o[0]!==""||o[1]!==""?(this._$AH=Array(o.length-1).fill(new String),this.strings=o):this._$AH=j}get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}_$AI(t,e=this,o,r){let s=this.strings,a=!1;if(s===void 0)t=et(this,t,e,0),a=!ft(t)||t!==this._$AH&&t!==W,a&&(this._$AH=t);else{let h=t,n,l;for(t=s[0],n=0;n<s.length-1;n++)l=et(this,h[o+n],e,n),l===W&&(l=this._$AH[n]),a||(a=!ft(l)||l!==this._$AH[n]),l===j?t=j:t!==j&&(t+=(l??"")+s[n+1]),this._$AH[n]=l}a&&!r&&this.j(t)}j(t){t===j?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,t??"")}},je=class extends ot{constructor(){super(...arguments),this.type=3}j(t){this.element[this.name]=t===j?void 0:t}},ii=tt?tt.emptyScript:"",$e=class extends ot{constructor(){super(...arguments),this.type=4}j(t){t&&t!==j?this.element.setAttribute(this.name,ii):this.element.removeAttribute(this.name)}},xe=class extends ot{constructor(t,e,o,r,s){super(t,e,o,r,s),this.type=5}_$AI(t,e=this){var o;if((t=(o=et(this,t,e,0))!==null&&o!==void 0?o:j)===W)return;let r=this._$AH,s=t===j&&r!==j||t.capture!==r.capture||t.once!==r.once||t.passive!==r.passive,a=t!==j&&(r===j||s);s&&this.element.removeEventListener(this.name,this,r),a&&this.element.addEventListener(this.name,this,t),this._$AH=t}handleEvent(t){var e,o;typeof this._$AH=="function"?this._$AH.call((o=(e=this.options)===null||e===void 0?void 0:e.host)!==null&&o!==void 0?o:this.element,t):this._$AH.handleEvent(t)}},_e=class{constructor(t,e,o){this.element=t,this.type=6,this._$AN=void 0,this._$AM=e,this.options=o}get _$AU(){return this._$AM._$AU}_$AI(t){et(this,t)}};var sr=Ot.litHtmlPolyfillSupport;sr?.(yt,jt),((ge=Ot.litHtmlVersions)!==null&&ge!==void 0?ge:Ot.litHtmlVersions=[]).push("2.8.0");var dr=(i,t,e)=>{var o,r;let s=(o=e?.renderBefore)!==null&&o!==void 0?o:t,a=s._$litPart$;if(a===void 0){let h=(r=e?.renderBefore)!==null&&r!==void 0?r:null;s._$litPart$=a=new jt(t.insertBefore(mt(),h),h,void 0,e??{})}return a._$AI(i),a};var we,ke;var f=class extends L{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){var t,e;let o=super.createRenderRoot();return(t=(e=this.renderOptions).renderBefore)!==null&&t!==void 0||(e.renderBefore=o.firstChild),o}update(t){let e=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(t),this._$Do=dr(e,this.renderRoot,this.renderOptions)}connectedCallback(){var t;super.connectedCallback(),(t=this._$Do)===null||t===void 0||t.setConnected(!0)}disconnectedCallback(){var t;super.disconnectedCallback(),(t=this._$Do)===null||t===void 0||t.setConnected(!1)}render(){return W}};f.finalized=!0,f._$litElement$=!0,(we=globalThis.litElementHydrateSupport)===null||we===void 0||we.call(globalThis,{LitElement:f});var pr=globalThis.litElementPolyfillSupport;pr?.({LitElement:f});((ke=globalThis.litElementVersions)!==null&&ke!==void 0?ke:globalThis.litElementVersions=[]).push("3.3.3");var b=i=>i??j;var Ae=class extends f{#t;#e;static get styles(){return y`
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
    `}static get properties(){return{accessibleLabel:{type:String,attribute:"accessible-label"},accessibleValueText:{type:String,attribute:"accessible-valuetext"},hideValue:{type:Boolean,attribute:"hide-value"},indeterminate:{type:Boolean,reflect:!0},label:{type:String,reflect:!0},max:{type:Number},min:{type:Number},size:{type:String,reflect:!0},type:{type:String,reflect:!0},value:{type:Number}}}constructor(){super(),this.#t=this.attachInternals(),this.indeterminate=!1,this.label=null,this.size="medium",this.type="linear",this.hideValue=!1,this.#t.ariaLabel,this.#t.ariaValueMax,this.#t.ariaValueMin,this.#t.ariaValueNow,this.#t.ariaValueText,this.#t.role="progressbar",this.accessibleLabel=null,this.max=100,this.min=0,this.value=0,this.accessibleValueText=null}connectedCallback(){super.connectedCallback(),new MutationObserver(this.#o.bind(this)).observe(this,{attributeFilter:["indeterminate"]})}#o(){this.indeterminate&&(this.value=null)}#r(t,e){let o=this.#t[t];e!==o&&(this.#t[t]=e),this.requestUpdate(t,o)}get label(){return this.#e}set label(t){let e=this.label;t!==e&&(this.#e=t,this.accessibleLabel===null&&(this.accessibleLabel=t)),this.requestUpdate("label",e)}get accessibleLabel(){return this.#t.ariaLabel}set accessibleLabel(t){this.#r("ariaLabel",t)}get min(){return this.#t.ariaValueMin}set min(t){this.#r("ariaValueMin",t)}get max(){return this.#t.ariaValueMax}set max(t){this.#r("ariaValueMax",t)}get accessibleValueText(){return this.#t.ariaValueText}set accessibleValueText(t){this.#r("ariaValueText",t)}get value(){return this.#t.ariaValueNow}set value(t){this.#r("ariaValueNow",t)}#i(t,e,o){let r;e?r=75:o===null?r=0:r=o;let s={small:[6,6,37.8],medium:[8,8,50.3],large:[16,16,100.8]},[a,h,n]=s[t],l=n-n*Number(r)/100;return d`
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
    `}#s(t){return d`
      <div class="linear-progress-bar">
        <div
          class="linear-progress-bar-value"
          style=${b(this.indeterminate?null:`width:${t||0}%`)}
        ></div>
      </div>
    `}render(){let t,e,o,r,s=Math.round((+this.value-+this.min)/(+this.max-+this.min)*100);return this.label&&(r=d`<label>${this.label}</label>`),this.type==="linear"?t=this.#s(s):this.type==="circular"&&(t=this.#i(this.size,this.indeterminate,s)),this.value&&!this.hideValue&&!this.indeterminate&&(this.label&&this.type==="linear"&&(o="has-label"),e=d`
        <span class=${b(o)}>${s+"%"}</span>
      `),d`
      <div class="container">
        ${this.label||this.value&&!this.hideValue?d`<div class="text-content">${r} ${e}</div>`:null}
        ${t}
      </div>
    `}};customElements.define("jh-progress",Ae);var Se=class extends f{static get formAssociated(){return!0}#t;#e;static get styles(){return y`
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
    `}static get properties(){return{accessibleDisabled:{type:String,attribute:"accessible-disabled",reflect:!0},accessibleLabel:{type:String,attribute:"accessible-label"},appearance:{type:String,reflect:!0},block:{type:Boolean},disabled:{type:Boolean,reflect:!0},href:{type:String},iconPosition:{type:String,attribute:"icon-position",reflect:!0},pending:{type:Boolean,reflect:!0},label:{type:String},name:{type:String},size:{type:String,reflect:!0},submit:{type:Boolean},target:{type:String},value:{type:String}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.form,this.accessibleDisabled=null,this.accessibleLabel=null,this.appearance="secondary",this.block=!1,this.disabled=!1,this.href=null,this.iconPosition="before",this.pending=!1,this.label=null,this.name=null,this.size="medium",this.submit=!1,this.target=null,this.value=null,this.addEventListener("click",this.#r),this.addEventListener("keydown",this.#i)}get value(){return this.#e}set value(t){let e=this.#e;t!==e&&(this.#e=t,this.#t.setFormValue(t)),this.requestUpdate("value",e)}firstUpdated(){this.#o(),new ResizeObserver(this.#o.bind(this)).observe(this)}#o(){let{width:t}=this.getBoundingClientRect();this.style.setProperty("--populated-button-width",`${t}px`)}formDisabledCallback(t){this.disabled=t}#r(t){this.submit&&this.#t.form&&!this.disabled&&this.#t.form.requestSubmit()}#i(t){(t.code==="Space"||t.code==="Enter")&&(this.accessibleDisabled==="true"||this.pending)&&t.preventDefault()}#s(){this.firstElementChild.setAttribute("aria-hidden","true"),this.firstElementChild.setAttribute("size","medium")}#a(t,e){let o,r;return e&&(r=d`<span>${e}</span>`),t&&!this.disabled?o=d`
        <jh-progress type="circular" indeterminate></jh-progress>
      `:o=d`
        <slot
          name="jh-button-icon"
          @slotchange=${this.#s}
        ></slot>
        ${r}
      `,d` <span class="content-wrapper">${o}</span> `}render(){let t=this.#a(this.pending,this.label),e;return this.accessibleDisabled!=="false"&&(e=this.accessibleDisabled),this.href?d`
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
      `:d`
        <button
          tabindex="0"
          aria-disabled=${b(e)}
          aria-label=${b(this.accessibleLabel)}
          ?disabled=${this.disabled}
          type=${this.submit?"submit":"button"}
        >
          ${t}
        </button>
      `}};customElements.define("jh-button",Se);var Ee=class extends f{static get styles(){return y`
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
      `}static get properties(){return{inset:{type:Number,reflect:!0}}}constructor(){super(),this.inset=null}};customElements.define("jh-divider",Ee);var ze=class extends f{static get styles(){return y`
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
    `}static get properties(){return{footerDividerInset:{type:Number,attribute:"footer-divider-inset"},headerDividerInset:{type:Number,attribute:"header-divider-inset"},showFooterDivider:{type:Boolean,attribute:"show-footer-divider",reflect:!0},showHeaderDivider:{type:Boolean,attribute:"show-header-divider",reflect:!0},padding:{type:String,reflect:!0},headerSubtitle:{type:String,attribute:"header-subtitle"},headerTitle:{type:String,attribute:"header-title"},titleHeadingLevel:{type:Number,attribute:"title-heading-level"}}}constructor(){super(),this.footerDividerInset=null,this.headerDividerInset=null,this.titleHeadingLevel=2,this.padding="medium",this.showFooterDivider=!1,this.showHeaderDivider=!1,this.headerSubtitle=null,this.headerTitle=null}#t(){switch(this.titleHeadingLevel){case 1:return d`<h1>${this.headerTitle}</h1>`;case 2:return d`<h2>${this.headerTitle}</h2>`;case 3:return d`<h3>${this.headerTitle}</h3>`;case 4:return d`<h4>${this.headerTitle}</h4>`;case 5:return d`<h5>${this.headerTitle}</h5>`;case 6:return d`<h6>${this.headerTitle}</h6>`;default:return d`<h2>${this.headerTitle}</h2>`}}#e(t){t.target.classList.add("display-slot")}render(){let t,e,o;return(this.headerTitle||this.headerSubtitle)&&(t=d`
        <div class="display-header">
          ${this.headerTitle?this.#t():null}
          ${this.headerSubtitle?d`<p>${this.headerSubtitle}</p>`:null}
        </div>
      `),this.showHeaderDivider&&(e=d`
        <jh-divider
          class="card-header-divider"
          inset=${b(this.headerDividerInset?this.headerDividerInset:null)}
        >
        </jh-divider>
      `),this.showFooterDivider&&(o=d`
        <jh-divider
          class="card-footer-divider"
          inset=${b(this.footerDividerInset?this.footerDividerInset:null)}
        >
        </jh-divider>
      `),d`
      <slot name="jh-card-media" @slotchange=${this.#e}></slot>
      <slot name="jh-card-header" @slotchange=${this.#e}>${t}</slot>
      ${e}
      <slot @slotchange=${this.#e}></slot>
      ${o}
      <slot name="jh-card-footer" @slotchange=${this.#e}></slot>
    `}};customElements.define("jh-card",ze);var Nt=window,Ht=Nt.ShadowRoot&&(Nt.ShadyCSS===void 0||Nt.ShadyCSS.nativeShadow)&&"adoptedStyleSheets"in Document.prototype&&"replace"in CSSStyleSheet.prototype,Ce=Symbol(),ur=new WeakMap,$t=class{constructor(t,e,o){if(this._$cssResult$=!0,o!==Ce)throw Error("CSSResult is not constructable. Use `unsafeCSS` or `css` instead.");this.cssText=t,this.t=e}get styleSheet(){let t=this.o,e=this.t;if(Ht&&t===void 0){let o=e!==void 0&&e.length===1;o&&(t=ur.get(e)),t===void 0&&((this.o=t=new CSSStyleSheet).replaceSync(this.cssText),o&&ur.set(e,t))}return t}toString(){return this.cssText}},br=i=>new $t(typeof i=="string"?i:i+"",void 0,Ce),S=(i,...t)=>{let e=i.length===1?i[0]:t.reduce((o,r,s)=>o+(a=>{if(a._$cssResult$===!0)return a.cssText;if(typeof a=="number")return a;throw Error("Value passed to 'css' function must be a 'css' function result: "+a+". Use 'unsafeCSS' to pass non-literal values, but take care to ensure page security.")})(r)+i[s+1],i[0]);return new $t(e,i,Ce)},Te=(i,t)=>{Ht?i.adoptedStyleSheets=t.map(e=>e instanceof CSSStyleSheet?e:e.styleSheet):t.forEach(e=>{let o=document.createElement("style"),r=Nt.litNonce;r!==void 0&&o.setAttribute("nonce",r),o.textContent=e.cssText,i.appendChild(o)})},Dt=Ht?i=>i:i=>i instanceof CSSStyleSheet?(t=>{let e="";for(let o of t.cssRules)e+=o.cssText;return br(e)})(i):i;var Le,Mt=window,vr=Mt.trustedTypes,si=vr?vr.emptyScript:"",gr=Mt.reactiveElementPolyfillSupport,Re={toAttribute(i,t){switch(t){case Boolean:i=i?si:null;break;case Object:case Array:i=i==null?i:JSON.stringify(i)}return i},fromAttribute(i,t){let e=i;switch(t){case Boolean:e=i!==null;break;case Number:e=i===null?null:Number(i);break;case Object:case Array:try{e=JSON.parse(i)}catch{e=null}}return e}},mr=(i,t)=>t!==i&&(t==t||i==i),Pe={attribute:!0,type:String,converter:Re,reflect:!1,hasChanged:mr},Ue="finalized",P=class extends HTMLElement{constructor(){super(),this._$Ei=new Map,this.isUpdatePending=!1,this.hasUpdated=!1,this._$El=null,this._$Eu()}static addInitializer(t){var e;this.finalize(),((e=this.h)!==null&&e!==void 0?e:this.h=[]).push(t)}static get observedAttributes(){this.finalize();let t=[];return this.elementProperties.forEach((e,o)=>{let r=this._$Ep(o,e);r!==void 0&&(this._$Ev.set(r,o),t.push(r))}),t}static createProperty(t,e=Pe){if(e.state&&(e.attribute=!1),this.finalize(),this.elementProperties.set(t,e),!e.noAccessor&&!this.prototype.hasOwnProperty(t)){let o=typeof t=="symbol"?Symbol():"__"+t,r=this.getPropertyDescriptor(t,o,e);r!==void 0&&Object.defineProperty(this.prototype,t,r)}}static getPropertyDescriptor(t,e,o){return{get(){return this[e]},set(r){let s=this[t];this[e]=r,this.requestUpdate(t,s,o)},configurable:!0,enumerable:!0}}static getPropertyOptions(t){return this.elementProperties.get(t)||Pe}static finalize(){if(this.hasOwnProperty(Ue))return!1;this[Ue]=!0;let t=Object.getPrototypeOf(this);if(t.finalize(),t.h!==void 0&&(this.h=[...t.h]),this.elementProperties=new Map(t.elementProperties),this._$Ev=new Map,this.hasOwnProperty("properties")){let e=this.properties,o=[...Object.getOwnPropertyNames(e),...Object.getOwnPropertySymbols(e)];for(let r of o)this.createProperty(r,e[r])}return this.elementStyles=this.finalizeStyles(this.styles),!0}static finalizeStyles(t){let e=[];if(Array.isArray(t)){let o=new Set(t.flat(1/0).reverse());for(let r of o)e.unshift(Dt(r))}else t!==void 0&&e.push(Dt(t));return e}static _$Ep(t,e){let o=e.attribute;return o===!1?void 0:typeof o=="string"?o:typeof t=="string"?t.toLowerCase():void 0}_$Eu(){var t;this._$E_=new Promise(e=>this.enableUpdating=e),this._$AL=new Map,this._$Eg(),this.requestUpdate(),(t=this.constructor.h)===null||t===void 0||t.forEach(e=>e(this))}addController(t){var e,o;((e=this._$ES)!==null&&e!==void 0?e:this._$ES=[]).push(t),this.renderRoot!==void 0&&this.isConnected&&((o=t.hostConnected)===null||o===void 0||o.call(t))}removeController(t){var e;(e=this._$ES)===null||e===void 0||e.splice(this._$ES.indexOf(t)>>>0,1)}_$Eg(){this.constructor.elementProperties.forEach((t,e)=>{this.hasOwnProperty(e)&&(this._$Ei.set(e,this[e]),delete this[e])})}createRenderRoot(){var t;let e=(t=this.shadowRoot)!==null&&t!==void 0?t:this.attachShadow(this.constructor.shadowRootOptions);return Te(e,this.constructor.elementStyles),e}connectedCallback(){var t;this.renderRoot===void 0&&(this.renderRoot=this.createRenderRoot()),this.enableUpdating(!0),(t=this._$ES)===null||t===void 0||t.forEach(e=>{var o;return(o=e.hostConnected)===null||o===void 0?void 0:o.call(e)})}enableUpdating(t){}disconnectedCallback(){var t;(t=this._$ES)===null||t===void 0||t.forEach(e=>{var o;return(o=e.hostDisconnected)===null||o===void 0?void 0:o.call(e)})}attributeChangedCallback(t,e,o){this._$AK(t,o)}_$EO(t,e,o=Pe){var r;let s=this.constructor._$Ep(t,o);if(s!==void 0&&o.reflect===!0){let a=(((r=o.converter)===null||r===void 0?void 0:r.toAttribute)!==void 0?o.converter:Re).toAttribute(e,o.type);this._$El=t,a==null?this.removeAttribute(s):this.setAttribute(s,a),this._$El=null}}_$AK(t,e){var o;let r=this.constructor,s=r._$Ev.get(t);if(s!==void 0&&this._$El!==s){let a=r.getPropertyOptions(s),h=typeof a.converter=="function"?{fromAttribute:a.converter}:((o=a.converter)===null||o===void 0?void 0:o.fromAttribute)!==void 0?a.converter:Re;this._$El=s,this[s]=h.fromAttribute(e,a.type),this._$El=null}}requestUpdate(t,e,o){let r=!0;t!==void 0&&(((o=o||this.constructor.getPropertyOptions(t)).hasChanged||mr)(this[t],e)?(this._$AL.has(t)||this._$AL.set(t,e),o.reflect===!0&&this._$El!==t&&(this._$EC===void 0&&(this._$EC=new Map),this._$EC.set(t,o))):r=!1),!this.isUpdatePending&&r&&(this._$E_=this._$Ej())}async _$Ej(){this.isUpdatePending=!0;try{await this._$E_}catch(e){Promise.reject(e)}let t=this.scheduleUpdate();return t!=null&&await t,!this.isUpdatePending}scheduleUpdate(){return this.performUpdate()}performUpdate(){var t;if(!this.isUpdatePending)return;this.hasUpdated,this._$Ei&&(this._$Ei.forEach((r,s)=>this[s]=r),this._$Ei=void 0);let e=!1,o=this._$AL;try{e=this.shouldUpdate(o),e?(this.willUpdate(o),(t=this._$ES)===null||t===void 0||t.forEach(r=>{var s;return(s=r.hostUpdate)===null||s===void 0?void 0:s.call(r)}),this.update(o)):this._$Ek()}catch(r){throw e=!1,this._$Ek(),r}e&&this._$AE(o)}willUpdate(t){}_$AE(t){var e;(e=this._$ES)===null||e===void 0||e.forEach(o=>{var r;return(r=o.hostUpdated)===null||r===void 0?void 0:r.call(o)}),this.hasUpdated||(this.hasUpdated=!0,this.firstUpdated(t)),this.updated(t)}_$Ek(){this._$AL=new Map,this.isUpdatePending=!1}get updateComplete(){return this.getUpdateComplete()}getUpdateComplete(){return this._$E_}shouldUpdate(t){return!0}update(t){this._$EC!==void 0&&(this._$EC.forEach((e,o)=>this._$EO(o,this[o],e)),this._$EC=void 0),this._$Ek()}updated(t){}firstUpdated(t){}};P[Ue]=!0,P.elementProperties=new Map,P.elementStyles=[],P.shadowRootOptions={mode:"open"},gr?.({ReactiveElement:P}),((Le=Mt.reactiveElementVersions)!==null&&Le!==void 0?Le:Mt.reactiveElementVersions=[]).push("1.6.3");var Oe,It=window,rt=It.trustedTypes,fr=rt?rt.createPolicy("lit-html",{createHTML:i=>i}):void 0,He="$lit$",N=`lit$${(Math.random()+"").slice(9)}$`,kr="?"+N,ai=`<${kr}>`,Z=document,_t=()=>Z.createComment(""),wt=i=>i===null||typeof i!="object"&&typeof i!="function",Ar=Array.isArray,ni=i=>Ar(i)||typeof i?.[Symbol.iterator]=="function",Ne=`[ 	
\f\r]`,xt=/<(?:(!--|\/[^a-zA-Z])|(\/?[a-zA-Z][^>\s]*)|(\/?$))/g,yr=/-->/g,jr=/>/g,K=RegExp(`>|${Ne}(?:([^\\s"'>=/]+)(${Ne}*=${Ne}*(?:[^ 	
\f\r"'\`<>=]|("|')|))|$)`,"g"),$r=/'/g,xr=/"/g,Sr=/^(?:script|style|textarea|title)$/i,Er=i=>(t,...e)=>({_$litType$:i,strings:t,values:e}),C=Er(1),Cs=Er(2),G=Symbol.for("lit-noChange"),$=Symbol.for("lit-nothing"),_r=new WeakMap,F=Z.createTreeWalker(Z,129,null,!1);function zr(i,t){if(!Array.isArray(i)||!i.hasOwnProperty("raw"))throw Error("invalid template strings array");return fr!==void 0?fr.createHTML(t):t}var li=(i,t)=>{let e=i.length-1,o=[],r,s=t===2?"<svg>":"",a=xt;for(let h=0;h<e;h++){let n=i[h],l,u,p=-1,v=0;for(;v<n.length&&(a.lastIndex=v,u=a.exec(n),u!==null);)v=a.lastIndex,a===xt?u[1]==="!--"?a=yr:u[1]!==void 0?a=jr:u[2]!==void 0?(Sr.test(u[2])&&(r=RegExp("</"+u[2],"g")),a=K):u[3]!==void 0&&(a=K):a===K?u[0]===">"?(a=r??xt,p=-1):u[1]===void 0?p=-2:(p=a.lastIndex-u[2].length,l=u[1],a=u[3]===void 0?K:u[3]==='"'?xr:$r):a===xr||a===$r?a=K:a===yr||a===jr?a=xt:(a=K,r=void 0);let m=a===K&&i[h+1].startsWith("/>")?" ":"";s+=a===xt?n+ai:p>=0?(o.push(l),n.slice(0,p)+He+n.slice(p)+N+m):n+N+(p===-2?(o.push(void 0),h):m)}return[zr(i,s+(i[e]||"<?>")+(t===2?"</svg>":"")),o]},kt=class i{constructor({strings:t,_$litType$:e},o){let r;this.parts=[];let s=0,a=0,h=t.length-1,n=this.parts,[l,u]=li(t,e);if(this.el=i.createElement(l,o),F.currentNode=this.el.content,e===2){let p=this.el.content,v=p.firstChild;v.remove(),p.append(...v.childNodes)}for(;(r=F.nextNode())!==null&&n.length<h;){if(r.nodeType===1){if(r.hasAttributes()){let p=[];for(let v of r.getAttributeNames())if(v.endsWith(He)||v.startsWith(N)){let m=u[a++];if(p.push(v),m!==void 0){let k=r.getAttribute(m.toLowerCase()+He).split(N),w=/([.?@])?(.*)/.exec(m);n.push({type:1,index:s,name:w[2],strings:k,ctor:w[1]==="."?Me:w[1]==="?"?Ie:w[1]==="@"?Be:st})}else n.push({type:6,index:s})}for(let v of p)r.removeAttribute(v)}if(Sr.test(r.tagName)){let p=r.textContent.split(N),v=p.length-1;if(v>0){r.textContent=rt?rt.emptyScript:"";for(let m=0;m<v;m++)r.append(p[m],_t()),F.nextNode(),n.push({type:2,index:++s});r.append(p[v],_t())}}}else if(r.nodeType===8)if(r.data===kr)n.push({type:2,index:s});else{let p=-1;for(;(p=r.data.indexOf(N,p+1))!==-1;)n.push({type:7,index:s}),p+=N.length-1}s++}}static createElement(t,e){let o=Z.createElement("template");return o.innerHTML=t,o}};function it(i,t,e=i,o){var r,s,a,h;if(t===G)return t;let n=o!==void 0?(r=e._$Co)===null||r===void 0?void 0:r[o]:e._$Cl,l=wt(t)?void 0:t._$litDirective$;return n?.constructor!==l&&((s=n?._$AO)===null||s===void 0||s.call(n,!1),l===void 0?n=void 0:(n=new l(i),n._$AT(i,e,o)),o!==void 0?((a=(h=e)._$Co)!==null&&a!==void 0?a:h._$Co=[])[o]=n:e._$Cl=n),n!==void 0&&(t=it(i,n._$AS(i,t.values),n,o)),t}var De=class{constructor(t,e){this._$AV=[],this._$AN=void 0,this._$AD=t,this._$AM=e}get parentNode(){return this._$AM.parentNode}get _$AU(){return this._$AM._$AU}u(t){var e;let{el:{content:o},parts:r}=this._$AD,s=((e=t?.creationScope)!==null&&e!==void 0?e:Z).importNode(o,!0);F.currentNode=s;let a=F.nextNode(),h=0,n=0,l=r[0];for(;l!==void 0;){if(h===l.index){let u;l.type===2?u=new At(a,a.nextSibling,this,t):l.type===1?u=new l.ctor(a,l.name,l.strings,this,t):l.type===6&&(u=new qe(a,this,t)),this._$AV.push(u),l=r[++n]}h!==l?.index&&(a=F.nextNode(),h++)}return F.currentNode=Z,s}v(t){let e=0;for(let o of this._$AV)o!==void 0&&(o.strings!==void 0?(o._$AI(t,o,e),e+=o.strings.length-2):o._$AI(t[e])),e++}},At=class i{constructor(t,e,o,r){var s;this.type=2,this._$AH=$,this._$AN=void 0,this._$AA=t,this._$AB=e,this._$AM=o,this.options=r,this._$Cp=(s=r?.isConnected)===null||s===void 0||s}get _$AU(){var t,e;return(e=(t=this._$AM)===null||t===void 0?void 0:t._$AU)!==null&&e!==void 0?e:this._$Cp}get parentNode(){let t=this._$AA.parentNode,e=this._$AM;return e!==void 0&&t?.nodeType===11&&(t=e.parentNode),t}get startNode(){return this._$AA}get endNode(){return this._$AB}_$AI(t,e=this){t=it(this,t,e),wt(t)?t===$||t==null||t===""?(this._$AH!==$&&this._$AR(),this._$AH=$):t!==this._$AH&&t!==G&&this._(t):t._$litType$!==void 0?this.g(t):t.nodeType!==void 0?this.$(t):ni(t)?this.T(t):this._(t)}k(t){return this._$AA.parentNode.insertBefore(t,this._$AB)}$(t){this._$AH!==t&&(this._$AR(),this._$AH=this.k(t))}_(t){this._$AH!==$&&wt(this._$AH)?this._$AA.nextSibling.data=t:this.$(Z.createTextNode(t)),this._$AH=t}g(t){var e;let{values:o,_$litType$:r}=t,s=typeof r=="number"?this._$AC(t):(r.el===void 0&&(r.el=kt.createElement(zr(r.h,r.h[0]),this.options)),r);if(((e=this._$AH)===null||e===void 0?void 0:e._$AD)===s)this._$AH.v(o);else{let a=new De(s,this),h=a.u(this.options);a.v(o),this.$(h),this._$AH=a}}_$AC(t){let e=_r.get(t.strings);return e===void 0&&_r.set(t.strings,e=new kt(t)),e}T(t){Ar(this._$AH)||(this._$AH=[],this._$AR());let e=this._$AH,o,r=0;for(let s of t)r===e.length?e.push(o=new i(this.k(_t()),this.k(_t()),this,this.options)):o=e[r],o._$AI(s),r++;r<e.length&&(this._$AR(o&&o._$AB.nextSibling,r),e.length=r)}_$AR(t=this._$AA.nextSibling,e){var o;for((o=this._$AP)===null||o===void 0||o.call(this,!1,!0,e);t&&t!==this._$AB;){let r=t.nextSibling;t.remove(),t=r}}setConnected(t){var e;this._$AM===void 0&&(this._$Cp=t,(e=this._$AP)===null||e===void 0||e.call(this,t))}},st=class{constructor(t,e,o,r,s){this.type=1,this._$AH=$,this._$AN=void 0,this.element=t,this.name=e,this._$AM=r,this.options=s,o.length>2||o[0]!==""||o[1]!==""?(this._$AH=Array(o.length-1).fill(new String),this.strings=o):this._$AH=$}get tagName(){return this.element.tagName}get _$AU(){return this._$AM._$AU}_$AI(t,e=this,o,r){let s=this.strings,a=!1;if(s===void 0)t=it(this,t,e,0),a=!wt(t)||t!==this._$AH&&t!==G,a&&(this._$AH=t);else{let h=t,n,l;for(t=s[0],n=0;n<s.length-1;n++)l=it(this,h[o+n],e,n),l===G&&(l=this._$AH[n]),a||(a=!wt(l)||l!==this._$AH[n]),l===$?t=$:t!==$&&(t+=(l??"")+s[n+1]),this._$AH[n]=l}a&&!r&&this.j(t)}j(t){t===$?this.element.removeAttribute(this.name):this.element.setAttribute(this.name,t??"")}},Me=class extends st{constructor(){super(...arguments),this.type=3}j(t){this.element[this.name]=t===$?void 0:t}},hi=rt?rt.emptyScript:"",Ie=class extends st{constructor(){super(...arguments),this.type=4}j(t){t&&t!==$?this.element.setAttribute(this.name,hi):this.element.removeAttribute(this.name)}},Be=class extends st{constructor(t,e,o,r,s){super(t,e,o,r,s),this.type=5}_$AI(t,e=this){var o;if((t=(o=it(this,t,e,0))!==null&&o!==void 0?o:$)===G)return;let r=this._$AH,s=t===$&&r!==$||t.capture!==r.capture||t.once!==r.once||t.passive!==r.passive,a=t!==$&&(r===$||s);s&&this.element.removeEventListener(this.name,this,r),a&&this.element.addEventListener(this.name,this,t),this._$AH=t}handleEvent(t){var e,o;typeof this._$AH=="function"?this._$AH.call((o=(e=this.options)===null||e===void 0?void 0:e.host)!==null&&o!==void 0?o:this.element,t):this._$AH.handleEvent(t)}},qe=class{constructor(t,e,o){this.element=t,this.type=6,this._$AN=void 0,this._$AM=e,this.options=o}get _$AU(){return this._$AM._$AU}_$AI(t){it(this,t)}};var wr=It.litHtmlPolyfillSupport;wr?.(kt,At),((Oe=It.litHtmlVersions)!==null&&Oe!==void 0?Oe:It.litHtmlVersions=[]).push("2.8.0");var Cr=(i,t,e)=>{var o,r;let s=(o=e?.renderBefore)!==null&&o!==void 0?o:t,a=s._$litPart$;if(a===void 0){let h=(r=e?.renderBefore)!==null&&r!==void 0?r:null;s._$litPart$=a=new At(t.insertBefore(_t(),h),h,void 0,e??{})}return a._$AI(i),a};var Ve,We;var _=class extends P{constructor(){super(...arguments),this.renderOptions={host:this},this._$Do=void 0}createRenderRoot(){var t,e;let o=super.createRenderRoot();return(t=(e=this.renderOptions).renderBefore)!==null&&t!==void 0||(e.renderBefore=o.firstChild),o}update(t){let e=this.render();this.hasUpdated||(this.renderOptions.isConnected=this.isConnected),super.update(t),this._$Do=Cr(e,this.renderRoot,this.renderOptions)}connectedCallback(){var t;super.connectedCallback(),(t=this._$Do)===null||t===void 0||t.setConnected(!0)}disconnectedCallback(){var t;super.disconnectedCallback(),(t=this._$Do)===null||t===void 0||t.setConnected(!1)}render(){return G}};_.finalized=!0,_._$litElement$=!0,(Ve=globalThis.litElementHydrateSupport)===null||Ve===void 0||Ve.call(globalThis,{LitElement:_});var Tr=globalThis.litElementPolyfillSupport;Tr?.({LitElement:_});((We=globalThis.litElementVersions)!==null&&We!==void 0?We:globalThis.litElementVersions=[]).push("3.3.3");var Bt=class extends _{#t;static get styles(){return S`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return C`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M6.53 5.47a.75.75 0 0 0-1.06 1.06L10.94 12l-5.47 5.47a.75.75 0 0 0 1.06 1.06L12 13.06l5.47 5.47a.75.75 0 0 0 1.06-1.06L13.06 12l5.47-5.47a.75.75 0 1 0-1.06-1.06L12 10.94 6.53 5.47Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-xmark",Bt);var ci=0,Lr="open",Ke=class extends f{#t;static get styles(){return y`
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
    `}static get properties(){return{flipDisabled:{type:Boolean,attribute:"flip-disabled"},label:{type:String},open:{type:Boolean,reflect:!0},position:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.flipDisabled=!1,this.label=null,this.open=!1,this.position="top-center"}connectedCallback(){super.connectedCallback(),this.id=`tooltip-describedby-${ci++}`;let t=new MutationObserver(this.#e.bind(this)),e={childList:!0};t.observe(this.shadowRoot,e)}#e(t){for(let e of t)Array.from(e.addedNodes).some(s=>s.tagName==="SPAN")&&(this.#t.role="tooltip",this.addEventListener("focus",this.#i,!0),this.addEventListener("mouseenter",this.#i),this.addEventListener("blur",this.#s,!0),this.addEventListener("mouseleave",this.#s),this.addEventListener("keydown",this.#r)),e.removedNodes.length>0&&(this.#t.role="",this.removeEventListener("focus",this.#i),this.removeEventListener("mouseenter",this.#i),this.removeEventListener("blur",this.#s),this.removeEventListener("mouseleave",this.#s),this.removeEventListener("keydown",this.#r))}disconnectedCallback(){super.disconnectedCallback(),this.removeEventListener("focus",this.#i),this.removeEventListener("mouseenter",this.#i),this.removeEventListener("blur",this.#s),this.removeEventListener("mouseleave",this.#s),this.removeEventListener("keydown",this.#r)}#o(){let t=this.getAttribute("id");this.firstElementChild.setAttribute("aria-describedby",t);let e=window.getComputedStyle(this.firstElementChild).display;this.style.setProperty("display",e)}updated(){this.open&&this.#a()}#r(t){t.key==="Escape"&&this.#s()}#i(){this.setAttribute(Lr,"")}#s(){this.removeAttribute(Lr)}#a(){let t=this.getAttribute("position");if(["left","right","top-start","top-end","top-center","bottom-start","bottom-end","bottom-center"].includes(t)&&this.flipDisabled===!1&&this.label){let[e,o]=t.split("-"),r=this.#h(),s=n=>t===n,a=n=>e===n.split("-")[0],h=n=>{let[l,u]=n.split("-");return t==="left"&&n==="right"||t==="right"&&n==="left"||e==="bottom"&&l==="top"&&o===u||e==="top"&&l==="bottom"&&o===u};if(!r.some(s)){let n=r.find(a)||r.find(h)||r[0];this.setAttribute("position",n)}}}#h(){let{tooltipWidth:t,tooltipHeight:e,elemHeight:o,elemWidth:r}=this.#c(),{elemLeft:s,elemRight:a,elemTop:h,elemBottom:n}=this.#n(),l=h-e>0,u=n-e>0,p=a-t>0,v=s-t>0,m=this.#l(h,o,e),k=this.#l(n,o,e),w=window.innerWidth-s-t>0,po=s>0,uo=this.#l(a,r,t),bo=this.#l(s,r,t),vo=a>0,go=s+r-t>0;return Object.entries({left:v&&m&&k,right:p&&m&&k,"top-start":l&&w&&po,"bottom-start":u&&w&&po,"top-center":l&&uo&&bo,"bottom-center":u&&uo&&bo,"top-end":l&&vo&&go,"bottom-end":u&&vo&&go}).reduce((mo,[Hr,Dr])=>Dr?[...mo,Hr]:mo,[])}#c(){return{tooltipWidth:this.shadowRoot.querySelector("span").getBoundingClientRect().width,tooltipHeight:this.shadowRoot.querySelector("span").getBoundingClientRect().height,elemHeight:this.getBoundingClientRect().height,elemWidth:this.getBoundingClientRect().width}}#n(){return{elemLeft:this.getBoundingClientRect().left,elemRight:window.innerWidth-this.getBoundingClientRect().right,elemTop:this.getBoundingClientRect().top,elemBottom:window.innerHeight-this.getBoundingClientRect().bottom}}#l(t,e,o){return t+e/2-o/2>0}render(){let t;return this.label&&(t=d`
        <span
          class=${b(this.open?"show":null)}
          aria-hidden=${this.open?"false":"true"}
        >
          ${this.label}
        </span>
      `),d`
      <slot @slotchange=${this.#o}></slot>
      ${t}
    `}};customElements.define("jh-tooltip",Ke);var Fe=class extends f{#t=!1;#e;static get styles(){return y`
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
    `}static get properties(){return{label:{type:String},dismissButtonAccessibleLabel:{type:String,attribute:"dismiss-button-accessible-label"},dismissible:{type:Boolean},tooltipLabel:{type:String,attribute:"tooltip-label"},size:{type:String,reflect:!0},href:{type:String},target:{type:String},removeOnDismiss:{type:Boolean,attribute:"remove-on-dismiss"}}}constructor(){super(),this.label=null,this.dismissible=!1,this.dismissButtonAccessibleLabel=null,this.tooltipLabel=null,this.size="small",this.href=null,this.target=null,this.removeOnDismiss=!1,this.addEventListener("keyup",this.#i),this.addEventListener("keyup",this.#o),this.addEventListener("blur",this.#s)}connectedCallback(){super.connectedCallback(),new ResizeObserver(this.#r.bind(this)).observe(this)}#o(t){(t.code==="Space"||t.code==="Enter")&&this.click()}#r(){if(!this.label)return;let t=this.#t,e=this.shadowRoot.querySelector(".label");this.#t=e.offsetWidth<e.scrollWidth,t!==this.#t&&(this.#t&&!this.href?(this.#e=this.getAttribute("tabindex"),this.setAttribute("tabindex","0")):this.#t||(this.#e?this.setAttribute("tabindex",this.#e):this.removeAttribute("tabindex")),this.update())}#i(t){if(t.target.#t&&(t.key==="Tab"||t.shiftKey&&t.key==="Tab")){let e=this.shadowRoot.querySelector("jh-tooltip");(t.composedPath()[0].localName==="jh-tag"||t.composedPath()[0].localName==="a")&&(e.open=!e.open),t.composedPath()[0].localName==="button"&&(e.open=!1)}}#s(t){if(t.target.#t){let e=this.shadowRoot.querySelector("jh-tooltip");e.open=!1}}#a(t){t&&(this.shadowRoot.querySelector('slot[name="jh-tag-icon"]').classList.add("display-icon-slot"),this.firstElementChild.setAttribute("size","x-small"))}#h(t){t.stopPropagation(),this.dispatchEvent(new CustomEvent("jh-dismiss",{bubbles:!0,cancelable:!0,composed:!0})),this.removeOnDismiss&&this.remove()}#c(){let t;return this.dismissible&&(t=d`
      <jh-button
        size="medium"
        appearance="secondary"
        accessible-label=${this.dismissButtonAccessibleLabel}
        @click=${this.#h}>
        <slot name="jh-tag-dismiss-icon" slot="jh-button-icon">
          <jh-icon-xmark slot="jh-button-icon"></jh-icon-xmark>
        </slot>
      </jh-button>
      `),this.dismissible&&this.tooltipLabel&&(t=d`<jh-tooltip label=${this.tooltipLabel}>${t}</jh-tooltip>`),t}#n(){let t,e=d`<span class="label">${this.label}</span>`;return this.#t&&(e=d`<jh-tooltip label=${this.label}>${e}</jh-tooltip>`),this.href?(this.removeAttribute("tabindex"),t=d` <a class="label-container" tabindex="0" href=${this.href}>${e}</a>`):t=d` <div class="label-container">${e}</div>`,t}render(){return d` 
    <span class="container">
      <slot name="jh-tag-icon" @slotchange=${this.#a}></slot>
      ${this.#n()}
      ${this.#c()}
    </span>
    `}};customElements.define("jh-tag",Fe);var qt=class extends _{#t;static get styles(){return S`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return C`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M4.625 10.375a5.75 5.75 0 1 1 11.5 0 5.75 5.75 0 0 1-11.5 0Zm5.75-7.25a7.25 7.25 0 1 0 4.569 12.88l4.65 4.65a.75.75 0 1 0 1.061-1.06l-4.65-4.651a7.25 7.25 0 0 0-5.63-11.819Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-magnifying-glass",qt);var Vt=class extends _{#t;static get styles(){return S`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return C`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M12 3.75a8.25 8.25 0 1 0 0 16.5 8.25 8.25 0 0 0 0-16.5ZM2.25 12c0-5.384 4.365-9.75 9.75-9.75s9.75 4.366 9.75 9.75c0 5.385-4.365 9.75-9.75 9.75S2.25 17.386 2.25 12Zm6.22-3.53a.75.75 0 0 1 1.06 0L12 10.94l2.47-2.47a.75.75 0 1 1 1.06 1.06L13.06 12l2.47 2.47a.75.75 0 1 1-1.06 1.06L12 13.062l-2.47 2.47a.75.75 0 0 1-1.06-1.06L10.94 12 8.47 9.53a.75.75 0 0 1 0-1.06Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-circle-xmark",Vt);var Wt=class extends _{#t;static get styles(){return S`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return C`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="M12 6.25c-4.354 0-6.76 2.428-9.066 4.754l-.464.466a.75.75 0 0 0 0 1.061l.464.467C5.24 15.324 7.646 17.751 12 17.751c4.354 0 6.76-2.427 9.066-4.753l.464-.467a.75.75 0 0 0 0-1.06l-.464-.467C18.76 8.678 16.354 6.25 12 6.25Zm0 10c-3.65 0-5.635-1.932-7.941-4.25C6.365 9.685 8.349 7.75 12 7.75c3.65 0 5.635 1.934 7.941 4.25-2.306 2.318-4.29 4.25-7.941 4.25ZM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-eye",Wt);var Kt=class extends _{#t;static get styles(){return S`
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
    `}static get properties(){return{size:{type:String,reflect:!0}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="graphics-symbol",this.#t.ariaHidden="true",this.size="medium"}render(){return C`
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">   <path fill-rule="evenodd" d="m4.672 4.673.708-.707 2.956 2.956c1.06-.42 2.261-.671 3.664-.671 4.354 0 6.76 2.427 9.066 4.753l.464.466a.75.75 0 0 1 0 1.061l-.464.467c-1.094 1.103-2.21 2.23-3.546 3.108l2.515 2.515-.707.708L4.672 4.673ZM6.48 7.895c-1.336.88-2.452 2.005-3.546 3.109l-.464.466a.75.75 0 0 0 0 1.061l.464.467C5.24 15.324 7.646 17.75 12 17.75c1.403 0 2.604-.252 3.664-.672l-1.176-1.176A8.57 8.57 0 0 1 12 16.25c-3.65 0-5.635-1.933-7.941-4.25 1.166-1.172 2.25-2.245 3.507-3.02L6.48 7.895Zm2.812 2.812a3 3 0 0 0 4 4.001l-4-4Zm.22-2.609 1.195 1.195a3 3 0 0 1 4 4l1.727 1.727c1.258-.775 2.341-1.848 3.507-3.02-2.306-2.316-4.29-4.25-7.941-4.25a8.57 8.57 0 0 0-2.488.348Z" clip-rule="evenodd"/> </svg> 
    `}};customElements.define("jh-icon-eye-slash",Kt);var di=0,Ze=class extends f{static get formAssociated(){return!0}#t;#e;#o;static get styles(){return y`
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
    `}static get properties(){return{accessibleLabelClearButton:{type:String,attribute:"accessible-label-clear-button"},accessibleLabelInput:{type:String,attribute:"accessible-label-input"},accessibleLabelHidePassword:{type:String,attribute:"accessible-label-hide-password"},accessibleLabelShowPassword:{type:String,attribute:"accessible-label-show-password"},autocomplete:{type:String},autoGrow:{type:Boolean,attribute:"auto-grow",reflect:!0},cols:{type:String},disabled:{type:Boolean,reflect:!0},enterkeyhint:{type:String},errorText:{type:String,attribute:"error-text"},helperText:{type:String,attribute:"helper-text",reflect:!0},hideLeftSlot:{type:Boolean,attribute:"hide-left-slot",reflect:!0},passwordVisible:{type:Boolean,attribute:"password-visible"},hideRightSlot:{type:Boolean,attribute:"hide-right-slot",reflect:!0},inputmode:{type:String},invalid:{type:Boolean,reflect:!0},label:{type:String},maxlength:{type:String},minlength:{type:String},name:{type:String},noResize:{type:Boolean,attribute:"no-resize",reflect:!0},placeholder:{type:String},readonly:{type:Boolean,reflect:!0},required:{type:Boolean,reflect:!0},rows:{type:String},showClearButton:{type:Boolean,attribute:"show-clear-button",reflect:!0},showIndicator:{type:Boolean,attribute:"show-indicator",reflect:!0},size:{type:String,reflect:!0},spellcheck:{type:String},type:{type:String,reflect:!0},value:{type:String},wrap:{type:String}}}constructor(){super(),this.#t=this.attachInternals(),this.accessibleLabelInput=null,this.accessibleLabelClearButton=null,this.accessibleLabelHidePassword=null,this.accessibleLabelShowPassword=null,this.autocomplete=null,this.autoGrow=!1,this.cols=null,this.disabled=!1,this.enterkeyhint=null,this.errorText=null,this.helperText=null,this.hideLeftSlot=!1,this.passwordVisible=!1,this.hideRightSlot=!1,this.inputmode=null,this.invalid=!1,this.label=null,this.maxlength=null,this.minlength=null,this.name=null,this.noResize=!1,this.placeholder=null,this.readonly=!1,this.required=!1,this.rows=null,this.showClearButton=!1,this.showIndicator=!1,this.size="medium",this.spellcheck=null,this.type="text",this.value=null,this.wrap=null}get form(){return this.#t.form}get validity(){return this.#t.validity}get value(){return this.#e}set value(t){let e=this.#e;t!==e&&(this.#e=t,this.#t.setFormValue(this.#e)),this.requestUpdate("value",e)}connectedCallback(){super.connectedCallback(),this.#o=di++}#r(){this.value=null,this.shadowRoot.querySelector("input").focus(),this.#i("jh-clear")}#i(t){this.dispatchEvent(new CustomEvent(t,{bubbles:!0,cancelable:!0,composed:!0}))}#s(){!this.readonly&&!this.disabled&&(this.passwordVisible=!this.passwordVisible)}#a(){let t=!1,e=!1;for(let o of this.children)o.slot==="jh-input-left"?o.assignedSlot!==null&&(t=!0):(o.slot==="jh-input-right"||o.slot==="jh-input-password-visible"||o.slot==="jh-input-password-hidden")&&o.assignedSlot!==null&&(e=!0);this.#h(t,e)}#h(t,e){let o,r=this.shadowRoot.querySelector("input");t&&e?o="input-both-slots":t?o="input-left-slot":e&&(o="input-right-slot"),o?(r.className&&r.classList.remove(r.className),r.classList.add(o)):r.classList.remove(r.className)}#c(){let t=this.shadowRoot.querySelector("textarea");t.style.height="auto",t.style.height=t.scrollHeight+"px"}#n(){this.#i("jh-change")}#l(t){this.value=t.target.value,this.type==="textarea"&&this.autoGrow&&this.#c()}#d(t){let e=t.target.value.substring(t.target.selectionStart,t.target.selectionEnd);this.dispatchEvent(new CustomEvent("jh-select",{detail:{selected:e},bubbles:!0,cancelable:!0,composed:!0}))}#p(){return d`
      <slot
        class="left-slot"
        name="jh-input-left"
        @slotchange=${this.#a}
        >${this.type==="search"?d`
              <jh-icon-magnifying-glass aria-hidden="true" size="small"></jh-icon-magnifying-glass>
            `:null}
      </slot>
    `}#u(){let t=this.value?null:"hide-clear-button";return d`
      <jh-button size="small" appearance="tertiary" class="clear-button ${t}" 
      accessible-label=${b(this.accessibleLabelClearButton)} 
      ?disabled=${this.disabled||this.readonly} 
      @click=${this.#r}
      >
        <slot name="jh-input-clear-button" slot="jh-button-icon">
          <jh-icon-circle-xmark slot="jh-button-icon" aria-hidden="true" size="small"></jh-icon-circle-xmark>
        </slot>
      </jh-button>
    `}#b(){if(this.type==="password"){let t=this.passwordVisible?this.accessibleLabelHidePassword:this.accessibleLabelShowPassword;return d`
        <jh-button
          size="small"
          class="right-slot"
          appearance="tertiary"
          ?disabled=${this.disabled||this.readonly}
          accessible-label=${b(t)}
          @click=${this.#s}
        >
          ${this.passwordVisible?d`
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
              `:d`
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
      `}else return d`
        <slot
          class="right-slot"
          name="jh-input-right"
          @slotchange=${this.#a}
        ></slot>
      `}#v(){if(this.readonly)return;let t=this.showClearButton&&!this.readonly&&!this.disabled,e=this.hideLeftSlot?null:this.#p(),o=t?this.#u():null,r;return this.hideRightSlot||this.type==="password"&&this.disabled?r=null:r=this.#b(),d`${e}${o}${r}`}#g(){if(this.label&&this.helperText&&this.errorText)return`jh-input-error-${this.#o} jh-input-desc-${this.#o}`;if(this.label&&this.helperText)return`jh-input-desc-${this.#o}`;if(this.invalid&&this.errorText)return`jh-input-error-${this.#o}`}render(){let t=["email","password","search","tel","text","textarea","url"],e,o,r,s,a,h,n=this.label&&this.helperText||this.invalid&&this.errorText?this.#g():null;return this.showIndicator&&(this.required?r=d`<span class="indicator" aria-hidden="true"> *</span>`:r=d`<span class="indicator"> (optional)</span>`),this.helperText&&(s=d`
        <p id="jh-input-desc-${this.#o}" class="helper-text">
          ${this.helperText}
        </p>
      `),this.label&&(e=d`
        <label for="jh-input-${this.#o}"> ${this.label}${r} </label>
        ${s}
      `),this.invalid&&this.errorText&&(a=d`
        <div id="jh-input-error-${this.#o}" class="error-text">
          ${this.errorText}
        </div>
      `),t.includes(this.type)&&(this.type==="password"&&this.passwordVisible?h="text":h=this.type),this.type==="textarea"?o=d`
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
      `:o=d`
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
      `,d` ${e} ${o} ${a} `}};customElements.define("jh-input",Ze);var pi=0,Ge=class extends f{static get formAssociated(){return!0}#t;#e;#o;#r;static get styles(){return y`
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
    `}static get properties(){return{accessibleLabel:{type:String,attribute:"accessible-label"},errorText:{type:String,attribute:"error-text"},helperText:{attribute:"helper-text"},invalid:{type:Boolean,reflect:!0},label:{type:String},name:{type:String},required:{type:Boolean,reflect:!0},orientation:{type:String,reflect:!0},showIndicator:{type:Boolean,reflect:!0,attribute:"show-indicator"},value:{type:String,reflect:!0}}}constructor(){super(),this.#o=this.attachInternals(),this.accessibleLabel=null,this.errorText=null,this.helperText=null,this.invalid=!1,this.label=null,this.name=null,this.required=!1,this.orientation="vertical",this.showIndicator=!1,this.value=null,this.addEventListener("jh-change",this.#a),this.addEventListener("keydown",this.#h),this.addEventListener("focusout",this.#c)}connectedCallback(){super.connectedCallback(),this.#e=pi++}get form(){return this.#o.form}get validity(){return this.#o.validity}get value(){return this.#r}set value(t){let e=this.#r;t!==e&&(this.#r=t,this.#o.setFormValue(t)),this.requestUpdate("value",e)}#i(){return[...this.querySelectorAll("jh-radio")]}#s(){let t=this.#i();if(this.#t=t.find(e=>e.value===this.value&&this.value!==null),!this.#t){let e=t.find(o=>o.checked);e?(this.value=e.value,this.#t=e):(this.value=null,this.#t=null)}this.#n(),this.#t||(t[0].tabIndex=0)}#a(t){if(t.target.tagName==="JH-RADIO"){this.value=t.target.value,this.#t=t.target,this.#n();let e={bubbles:!0,composed:!0,cancelable:!0};this.dispatchEvent(new CustomEvent("jh-change",e))}}#h(t){if(!["ArrowDown","ArrowRight","ArrowUp","ArrowLeft","Space"].includes(t.code))return;let o=this.#i().filter(a=>!a.disabled),r=o.length,s=o.findIndex(a=>a.checked);s=s===-1?0:s,t.code==="ArrowDown"||t.code==="ArrowRight"?(t.preventDefault(),s+1<r?s++:s=0):(t.code==="ArrowUp"||t.code==="ArrowLeft")&&(t.preventDefault(),s-1>=0?s--:s=r-1),this.value=o[s].value,o[s].click(),this.#t=o[s],this.#n(),o[s].focus()}#c(){let t=this.#i().filter(e=>!e.disabled);t.some(e=>e.checked)&&t.forEach(e=>e.checked?e.tabIndex=0:e.tabIndex=-1)}#n(){this.#i().forEach(e=>{e.checked=this.#t===e,e.checked?e.tabIndex=0:e.tabIndex=-1})}#l(){if(this.errorText&&this.invalid&&this.helperText&&this.label)return`radio-group-error-${this.#e} radio-group-helper-${this.#e}`;if(this.errorText&&this.invalid)return`radio-group-error-${this.#e}`;if(this.helperText&&this.label)return`radio-group-helper-${this.#e}`}render(){let t,e,o,r;return this.showIndicator&&(this.required?t=d`<span class="indicator" aria-hidden="true"> *</span>`:t=d`<span class="indicator"> (optional)</span>`),this.helperText&&(e=d`<p
        class="helper-text"
        id="radio-group-helper-${this.#e}"
      >
        ${this.helperText}
      </p>`),this.label&&(o=d`<legend class="label" for="radio-group-label-${this.#e}">
          ${this.label}${t}
        </legend>
        ${e}`),this.invalid&&this.errorText&&(r=d`<p class="error-text" id="radio-group-error-${this.#e}">
        ${this.errorText}
      </p>`),d`
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
    `}};customElements.define("jh-radio-group",Ge);var ui=0,Ye=class extends f{#t;#e;static get styles(){return y`
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
    `}static get properties(){return{accessibleLabel:{type:String,attribute:"accessible-label"},checked:{type:Boolean,reflect:!0},disabled:{type:Boolean,reflect:!0},helperText:{type:String,attribute:"helper-text"},label:{type:String},value:{type:String}}}constructor(){super(),this.#e=this.attachInternals(),this.#e.role="radio",this.accessibleLabel=null,this.checked=!1,this.disabled=!1,this.helperText=null,this.label=null,this.value=null,this.addEventListener("click",this.#r),this.addEventListener("keydown",this.#i)}connectedCallback(){super.connectedCallback(),this.#t=ui++,this.setAttribute("tabindex","0"),this.setAttribute("aria-checked",`${this.checked}`),this.accessibleLabel&&this.setAttribute("aria-label",`${this.accessibleLabel}`),this.helperText&&this.setAttribute("aria-describedby",`radio-helper-text-${this.#t}`);let t=new MutationObserver(this.#o.bind(this)),e={attributes:!0};t.observe(this,e)}#o(t){for(let e of t)e.attributeName==="checked"&&this.setAttribute("aria-checked",`${this.checked}`),e.attributeName==="disabled"&&this.setAttribute("aria-disabled",`${this.disabled}`),e.attributeName==="accessible-label"&&this.setAttribute("aria-label",`${this.accessibleLabel}`)}#r(){if(this.disabled||this.checked)return;this.checked=!0;let t={bubbles:!0,composed:!0,cancelable:!0};this.dispatchEvent(new CustomEvent("jh-change",t))}#i(t){t.code==="Space"&&this.#r()}render(){let t,e;return this.helperText&&(t=d`
        <p class="helper-text" id="radio-helper-text-${this.#t}">
          ${this.helperText}
        </p>
      `),this.label&&(e=d`
        <span class="label-container">
          <span class="label-text">${this.label}</span>
          ${t}
        </span>
      `),d`
      <span class="radio" aria-hidden="true"></span>
      ${e}
    `}};customElements.define("jh-radio",Ye);var Qe=class extends f{static get styles(){return y`
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
    `}static get properties(){return{appearance:{type:String,reflect:!0},dismissButtonAccessibleLabel:{type:String,attribute:"dismiss-button-accessible-label"},hideDismissButton:{type:Boolean,attribute:"hide-dismiss-button"},stacked:{type:Boolean,reflect:!0},type:{type:String,reflect:!0}}}constructor(){super(),this.appearance="neutral",this.dismissButtonAccessibleLabel=null,this.hideDismissButton=!1,this.stacked=!1,this.type="alert"}#t(){this.#e("jh-dismiss"),this.parentNode.host?.nodeName!=="JH-TOAST"&&this.remove()}#e(t){this.dispatchEvent(new CustomEvent(t,{bubbles:!0,cancelable:!0,composed:!0}))}#o(){let t=this.stacked?"stacked-action-btn":"inline-action-btn";return d`
        <slot name="jh-notification-action" class=${t} @slotchange=${this.#r}></slot>
    `}#r(){let t=this.querySelectorAll("jh-button");for(let e of t)e.getAttribute("size")!=="small"&&e.setAttribute("size","small")}#i(t){t&&this.shadowRoot.querySelector("slot:not([name])").classList.add("display-default-slot")}render(){let t;return this.hideDismissButton||(t=d`
        <jh-button
          size="small"
          appearance="secondary"
          accessible-label=${this.dismissButtonAccessibleLabel}
          @click=${this.#t}
          ><slot name="jh-notification-dismiss-icon" slot="jh-button-icon">
            <jh-icon-xmark slot="jh-button-icon"></jh-icon-xmark>
          </slot>
        </jh-button>
      `),d`
      <div class=${this.stacked?"stacked-container":"inline-container"}>
        <slot aria-hidden="true" name="jh-notification-icon"></slot>
        <slot @slotchange=${this.#i}></slot>
        ${this.stacked?null:this.#o()}
        ${t}
      </div>
      ${this.stacked?this.#o():null}
    `}};customElements.define("jh-notification",Qe);var bi=0,Xe=class extends f{#t;static get styles(){return y`
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
    `}static get properties(){return{label:{type:String},accessibleLabel:{type:String,attribute:"accessible-label"}}}constructor(){super(),this.label=null,this.accessibleLabel=null}connectedCallback(){super.connectedCallback(),this.#t=bi++}render(){return d`
      ${this.label?d`<div class="subheader" id="list-group-labelledby-${this.#t}">
            ${this.label}
          </div>`:null}
      <div
        role="group"
        aria-labelledby=${b(this.label?`list-group-labelledby-${this.#t}`:null)}
        aria-label=${b(this.accessibleLabel)}
      >
        <slot></slot>
      </div>
    `}};customElements.define("jh-list-group",Xe);var Je=class extends f{#t;static get styles(){return y`
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
    `}static get properties(){return{disabled:{type:Boolean,reflect:!0},dividerInset:{type:Number,attribute:"divider-inset"},primaryMetadata:{type:String,attribute:"primary-metadata"},primaryText:{type:String,attribute:"primary-text"},secondaryMetadata:{type:String,attribute:"secondary-metadata"},secondaryText:{type:String,attribute:"secondary-text"},selected:{type:Boolean,reflect:!0},showDivider:{type:Boolean,reflect:!0,attribute:"show-divider"}}}constructor(){super(),this.#t=this.attachInternals(),this.#t.role="listitem",this.disabled=!1,this.dividerInset=null,this.primaryMetadata=null,this.primaryText=null,this.secondaryMetadata=null,this.secondaryText=null,this.selected=!1,this.showDivider=!1,this.primaryTooltip=!1,this.secondaryTooltip=!1}#e(){let t=this.primaryTooltip,e=this.secondaryTooltip,o=this.shadowRoot.querySelector(".primary-text"),r=this.shadowRoot.querySelector(".secondary-text");this.primaryTooltip=o.offsetWidth<o.scrollWidth,this.secondaryTooltip=r.offsetWidth<r.scrollWidth,(t!==this.primaryTooltip||e!==this.secondaryTooltip)&&this.update()}async firstUpdated(){let t=this.shadowRoot.querySelectorAll("jh-tooltip"),e={characterData:!0,subtree:!0};await t.forEach(o=>{Promise.all(Array.from(o.children).map(r=>r.updateComplete)),new MutationObserver(this.#e.bind(this)).observe(o.children[0],e),new ResizeObserver(this.#e.bind(this)).observe(o.children[0])})}render(){let t;return this.disabled===!0&&this.hasAttribute("tabindex")?this.setAttribute("aria-disabled","true"):this.removeAttribute("aria-disabled"),this.showDivider&&(t=d`
        <jh-divider
          inset=${b(this.dividerInset?this.dividerInset:null)}
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
      ${t}
    `}};customElements.define("jh-list-item",Je);var Pr=Symbol(),E=class{get taskComplete(){return this.t||(this.i===1?this.t=new Promise((t,e)=>{this.o=t,this.h=e}):this.i===3?this.t=Promise.reject(this.l):this.t=Promise.resolve(this.u)),this.t}constructor(t,e,o){this.p=0,this.i=0,(this._=t).addController(this);let r=typeof e=="object"?e:{task:e,args:o};this.v=r.task,this.j=r.args,this.m=r.argsEqual??Rr,this.k=r.onComplete,this.A=r.onError,this.autoRun=r.autoRun??!0,"initialValue"in r&&(this.u=r.initialValue,this.i=2,this.O=this.T?.())}hostUpdate(){this.autoRun===!0&&this.S()}hostUpdated(){this.autoRun==="afterUpdate"&&this.S()}T(){if(this.j===void 0)return;let t=this.j();if(!Array.isArray(t))throw Error("The args function must return an array");return t}async S(){let t=this.T(),e=this.O;this.O=t,t===e||t===void 0||e!==void 0&&this.m(e,t)||await this.run(t)}async run(t){let e,o;t??=this.T(),this.O=t,this.i===1?this.q?.abort():(this.t=void 0,this.o=void 0,this.h=void 0),this.i=1,this.autoRun==="afterUpdate"?queueMicrotask(()=>this._.requestUpdate()):this._.requestUpdate();let r=++this.p;this.q=new AbortController;let s=!1;try{e=await this.v(t,{signal:this.q.signal})}catch(a){s=!0,o=a}if(this.p===r){if(e===Pr)this.i=0;else{if(s===!1){try{this.k?.(e)}catch{}this.i=2,this.o?.(e)}else{try{this.A?.(o)}catch{}this.i=3,this.h?.(o)}this.u=e,this.l=o}this._.requestUpdate()}}abort(t){this.i===1&&this.q?.abort(t)}get value(){return this.u}get error(){return this.l}get status(){return this.i}render(t){switch(this.i){case 0:return t.initial?.();case 1:return t.pending?.();case 2:return t.complete?.(this.value);case 3:return t.error?.(this.error);default:throw Error("Unexpected status: "+this.i)}}},Rr=(i,t)=>i===t||i.length===t.length&&i.every((e,o)=>!Ct(e,t[o]));var Y=class{events=[];connected=!1;constructor(t,{limit:e=100,onEvent:o,onSession:r,item:s}={}){this._host=t,this._limit=e,this._onEvent=o,this._onSession=r,this._item=s,t.addController(this)}hostConnected(){this._item||qo().then(t=>{this.events=(t??[]).slice(-this._limit),this._host.requestUpdate()}).catch(()=>{}),this._es=new EventSource(this._item?`/v1/events?item=${encodeURIComponent(this._item)}`:"/v1/events"),this._es.addEventListener("hello",()=>{this.connected=!0,this._host.requestUpdate()}),this._es.addEventListener("session",t=>{this._onSession?.(JSON.parse(t.data))}),this._es.addEventListener("activity",t=>{let e=JSON.parse(t.data);this.events=[...this.events,e].slice(-this._limit),this._host.requestUpdate(),this._onEvent?.(e)}),this._es.onerror=()=>{this.connected=!1,this._host.requestUpdate()}}hostDisconnected(){this._es?.close(),this._es=void 0}};function z(i){return i==null?"":i<90?`${i}s`:i<5400?`${Math.floor(i/60)}m`:i<172800?`${Math.floor(i/3600)}h`:`${Math.floor(i/86400)}d`}function vi(i){let t="";try{t=atob(i.Body??"")}catch{return i.Subject??""}if(!t)return i.Subject??"";let e=null;try{e=JSON.parse(t)}catch{}if(e&&typeof e=="object")switch(i.Kind){case"gate":return`${e.Gate}: ${e.Decision}${e.Reason?" \u2014 "+to(e.Reason):""}`;case"agent-run":return`${e.seat} as ${e.role}, mode ${e.mode}`;case"agent-result":{let o=[`${e.stop}`,`${Math.round((e.wall_ms??0)/100)/10}s`];return o.push(e.metered?`${eo(e.tokens)} tokens`:"cost NOT reported"),e.cost_usd&&o.push(`$${e.cost_usd.toFixed(4)}`),o.push(e.tools?Ur(e.tools):`${e.tool_calls} tools`),e.denied_tools&&o.push(`DENIED ${Ur(e.denied_tools)}`),e.unknown_updates?.length&&o.push(`dialect drift: ${e.unknown_updates.join(", ")}`),o.join(" \xB7 ")}case"authz":return e.reason??"";default:return to(t)}return to(t)}function to(i){let t=String(i).indexOf(`
`);return t<0?String(i):String(i).slice(0,t)}function Ur(i){return Object.entries(i).sort((t,e)=>e[1]-t[1]).map(([t,e])=>e>1?`${t}\xD7${e}`:t).join(" ")}function eo(i){return i==null?"0":i<1e3?String(i):i<1e6?`${Math.round(i/100)/10}k`:`${Math.round(i/1e5)/10}M`}function Ft(i){return i.route?i.key?`${i.route}/${i.key}`:i.route:i.ingress??""}function gi(i){return Object.keys(i??{}).sort().map(t=>`${t} ${i[t]}`).join(" \xB7 ")}var oo={live:["pending","claimed","waiting","handedoff"],ready:["pending","handedoff"],blocked:["pending","handedoff","waiting"],pending:["pending"],claimed:["claimed"],waiting:["waiting"],handedoff:["handedoff"],parked:["parked"],done:["done"],all:[]},mi={ready:i=>!!i.ready,blocked:i=>(i.blocked_by?.length??0)>0};function fi(i){if(!i.runner)return"";let t=i.lease_expires_sec??0;return i.lease_lapsed?`${i.runner} \u2014 lease LAPSED ${z(-t)} ago`:`${i.runner} \u2014 ${z(t)} left`}function Nr(i){return i.blocked_by?.length?i.blocked_by.join(", "):i.root?`cascaded from ${i.root}`:""}var Or=[{key:"state",label:"State",get:i=>i.state},{key:"work",label:"Work",get:i=>i.task||i.id},{key:"why",label:"Blocked by",get:i=>Nr(i)},{key:"stage",label:"Stage",get:i=>i.stage??""},{key:"holder",label:"Held by",get:i=>i.runner??""},{key:"origin",label:"Origin",get:i=>Ft(i)},{key:"age",label:"Age",get:i=>i.age_sec??0}];var ro=class extends x{static styles=A`
    :host { display: grid; gap: 16px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; }
    .stat { text-align: center; }
    .stat .n { font-size: 1.6rem; font-weight: 600; }
    .stat .l { color: var(--jh-color-content-secondary-enabled); font-size: .8rem; }
    .feed { max-height: 420px; overflow-y: auto; }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); }
    .idle { color: var(--jh-color-content-secondary-enabled); }
    .breakdown { font-size: .8rem; color: var(--jh-color-content-secondary-enabled); margin: 12px 0 0; }
  `;_status=new E(this,{task:async(t,{signal:e})=>le(e),args:()=>[]});_running=new E(this,{task:async(t,{signal:e})=>he(oo.live,e),args:()=>[]});_activity=new Y(this,{onEvent:()=>this._refresh()});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>{this._status.run(),this._running.run()},400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}render(){return c`
      <jh-card header-title="Factory status" padding="medium">
        ${this._status.render({pending:()=>c`<jh-progress accessible-label="Loading status"></jh-progress>`,error:t=>c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>c`
            <div class="grid">
              <div class="stat"><div class="n">${t.pending}</div><div class="l">pending</div></div>
              <div class="stat"><div class="n">${t.exceptions}</div><div class="l">exceptions</div></div>
              ${""}
              ${t.exception_queue?.parked?c`<div class="stat">
                    <div class="n">${z(t.exception_queue.oldest_age_sec)}</div>
                    <div class="l">oldest parked of ${z(t.exception_queue.objective_sec)}</div>
                  </div>`:g}
              <div class="stat"><div class="n">${t.stopped?"stopped":"running"}</div><div class="l">claims</div></div>
              ${t.spend?c`
                <div class="stat">
                  <div class="n">${eo(t.spend.tokens)}</div>
                  <div class="l">tokens${t.spend.max_tokens?` of ${eo(t.spend.max_tokens)}`:""}</div>
                </div>
                <div class="stat">
                  <div class="n">${t.spend.running}/${t.spend.max_concurrent}</div>
                  <div class="l">running</div>
                </div>`:g}
              <div class="stat"><div class="n">${t.routes.length}</div><div class="l">routes</div></div>
            </div>
            ${""}
            ${t.backlog?.total?c`<p class="breakdown">
                  Queue: ${gi(t.backlog.by_state)} — <a href="/work">list them</a>
                </p>`:g}
            ${""}
            ${t.spend?.unmetered?c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>
                  ${t.spend.unmetered} run(s) this window did not report a cost — the spend above is
                  incomplete and claims are stopped until the window turns.
                </jh-notification>`:g}
            ${t.spend?.refusing&&!t.spend.unmetered?c`<jh-notification appearance="neutral" type="banner" hide-dismiss-button>
                  The meter is pacing work: the factory is idle because it is over budget, not because
                  there is nothing to do.
                </jh-notification>`:g}
            ${t.exception_queue&&!t.exception_queue.met?c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>
                  ${t.exception_queue.overdue} exception(s) have been parked longer than the
                  ${z(t.exception_queue.objective_sec)} response-time objective — the oldest for
                  ${z(t.exception_queue.oldest_age_sec)}.
                </jh-notification>`:g}`})}
      </jh-card>
      <jh-card header-title="In flight" header-subtitle="What the factory is doing right now, unit by unit" padding="medium">
        ${this._running.render({pending:()=>c`<jh-progress accessible-label="Loading in-flight work"></jh-progress>`,error:t=>c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>(t?.length??0)===0?c`<p class="idle">Nothing in flight.</p>`:c`<jh-list-group accessible-label="In-flight work">
                ${t.map(e=>c`
                  <jh-list-item
                    primary-text=${e.task||e.id}
                    secondary-text=${e.note||Ft(e)}
                    primary-metadata=${e.stage??""}
                    secondary-metadata=${z(e.in_state_sec)+" in stage"}
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
            ${this._activity.events.slice().reverse().map(t=>c`
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
    `}};customElements.define("box-dashboard",ro);var io=class extends x{static styles=A`
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
    .wrap { overflow-x: auto; }
    table { border-collapse: collapse; width: 100%; font-size: .85rem; }
    caption { text-align: left; padding-bottom: 8px; color: var(--jh-color-content-secondary-enabled); font-size: .8rem; }
    th, td { text-align: left; vertical-align: top; padding: 6px 10px 6px 0;
             border-bottom: 1px solid var(--jh-color-border-primary-enabled, #e0e0e0); }
    th { white-space: nowrap; }
    thead th button {
      font: inherit; font-weight: 600; cursor: pointer; padding: 0; border: 0;
      background: transparent; color: inherit;
    }
    thead th[aria-sort="ascending"] button::after { content: ' \\2191'; }
    thead th[aria-sort="descending"] button::after { content: ' \\2193'; }
    .note { white-space: pre-wrap; font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
    .muted { color: var(--jh-color-content-secondary-enabled); }
    .ready { color: var(--jh-color-content-positive-enabled, #146c2e); font-weight: 600; }
    .lapsed { color: var(--jh-color-content-negative-enabled, #b3261e); font-weight: 600; }
    .empty { color: var(--jh-color-content-secondary-enabled); }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
  `;static properties={_filter:{state:!0},_sort:{state:!0},_desc:{state:!0}};constructor(){super(),this._filter="live",this._sort="",this._desc=!1,this._activity=new Y(this,{limit:1,onEvent:()=>this._refresh()})}_task=new E(this,{task:async([t],{signal:e})=>he(oo[t],e),args:()=>[this._filter]});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>this._task.run(),400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}_sortBy(t){if(this._sort===t){this._desc=!this._desc;return}this._sort=t,this._desc=!1}_rows(t){let e=mi[this._filter],o=(t??[]).filter(a=>e?e(a):!0),r=Or.find(a=>a.key===this._sort);if(!r)return o;let s=this._desc?-1:1;return o.slice().sort((a,h)=>{let n=r.get(a),l=r.get(h);return typeof n=="number"&&typeof l=="number"?(n-l)*s:String(n).localeCompare(String(l))*s})}render(){return c`
      <jh-card header-title="Work" header-subtitle="The frontier: what is runnable, what is blocked, and on what" padding="medium">
        <div class="filters" role="group" aria-label="Filter work">
          ${Object.keys(oo).map(t=>c`
            <button type="button" aria-pressed=${this._filter===t?"true":"false"}
              @click=${()=>{this._filter=t}}>${t}</button>`)}
        </div>
        <div class="live">${this._activity.connected?"live":"reconnecting\u2026"}</div>
        ${this._task.render({pending:()=>c`<jh-progress accessible-label="Loading work"></jh-progress>`,error:t=>c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>this._table(this._rows(t))})}
      </jh-card>`}_table(t){return t.length===0?c`<p class="empty">Nothing in this lane.</p>`:c`
      <div class="wrap">
        <table>
          <caption>${t.length} item(s), ${this._sort?`sorted by ${this._sort}`:"oldest first"}</caption>
          <thead>
            <tr>${Or.map(e=>c`
              <th scope="col" aria-sort=${this._sort===e.key?this._desc?"descending":"ascending":"none"}>
                <button type="button" @click=${()=>this._sortBy(e.key)}>${e.label}</button>
              </th>`)}
            </tr>
          </thead>
          <tbody>
            ${t.map(e=>c`
              <tr>
                <td>
                  <jh-tag size="small" label=${e.state}></jh-tag>
                  ${e.ready?c`<div class="ready">ready</div>`:g}
                </td>
                <th scope="row">
                  <a href=${"/items/"+e.id}>${e.task||e.id}</a>
                  ${e.note?c`<div class="note">${e.note}</div>`:g}
                </th>
                <td>${Nr(e)||c`<span class="muted">—</span>`}</td>
                <td>${e.stage??""}
                  ${e.stage?c`<div class="muted">${z(e.in_state_sec)}</div>`:g}</td>
                <td class=${e.lease_lapsed?"lapsed":""}>${fi(e)||c`<span class="muted">—</span>`}</td>
                <td class="muted">${Ft(e)}</td>
                <td>${z(e.age_sec)}</td>
              </tr>`)}
          </tbody>
        </table>
      </div>`}};customElements.define("box-work",io);function yi(i){switch(i.kind){case"started":return"agent started";case"tool":return i.tool||"(unnamed \u2014 nothing for policy to match)";case"denied":return`${i.tool} \u2014 REFUSED by policy at the stream`;case"escalate":return i.reason;case"handoff":return i.reason;default:return i.text??""}}var ji={denied:"negative",escalate:"negative",handoff:"neutral",started:"positive"},so=class extends x{static styles=A`
    :host { display: block; }
    .grid { display: grid; gap: 16px; grid-template-columns: repeat(auto-fit, minmax(380px, 1fr)); align-items: start; }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
    .empty { color: var(--jh-color-content-secondary-enabled); }
    .meta { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; font-size: .8rem;
            color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
    .layers { display: flex; gap: 4px; flex-wrap: wrap; margin-bottom: 8px; }
    .layer { font-size: .7rem; padding: 2px 6px; border-radius: 4px;
             background: var(--jh-color-container-secondary-enabled); }
    h4 { margin: 12px 0 4px; font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
    ol { list-style: none; margin: 0; padding: 0; max-height: 320px; overflow-y: auto; font-size: .8rem; }
    li { display: grid; grid-template-columns: 62px 70px 1fr; gap: 8px; padding: 3px 0;
         border-bottom: 1px solid var(--jh-color-border-primary-enabled, #eee); }
    li .t { color: var(--jh-color-content-secondary-enabled); font-variant-numeric: tabular-nums; }
    li .k { font-weight: 600; }
    li .x { white-space: pre-wrap; overflow-wrap: anywhere; }
    li.negative .k { color: var(--jh-color-content-negative-enabled, #b3261e); }
    .blind { color: var(--jh-color-content-secondary-enabled); font-style: italic; }
    code { font-size: .75rem; }
  `;static properties={_frames:{state:!0}};constructor(){super(),this._frames={},this._activity=new Y(this,{limit:1,onEvent:()=>this._refresh(),onSession:t=>{this._frames={...this._frames,[t.item]:[...this._frames[t.item]??[],t]}}})}_task=new E(this,{task:async(t,{signal:e})=>Vo(e),args:()=>[]});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>this._task.run(),400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}_timeline(t){let e=new Set((t.timeline??[]).map(o=>o.seq));return[...t.timeline??[],...(this._frames[t.id]??[]).filter(o=>!e.has(o.seq))]}render(){return c`
      <div class="live">${this._activity.connected?"live":"reconnecting\u2026"}</div>
      ${this._task.render({pending:()=>c`<jh-progress accessible-label="Loading sessions"></jh-progress>`,error:t=>c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>(t?.length??0)===0?c`<p class="empty">No unit is running. Claimed work appears here as it starts.</p>`:c`<div class="grid">${t.map(e=>this._card(e))}</div>`})}`}_card(t){let e=this._timeline(t);return c`
      <jh-card header-title=${t.task||t.id} header-subtitle=${Ft(t)} padding="medium">
        <div class="meta">
          <jh-tag size="small" label=${t.state}></jh-tag>
          ${t.stage?c`<jh-tag size="small" label=${t.stage}></jh-tag><span>${z(t.in_state_sec)} in stage</span>`:g}
          ${t.seat?c`<span>seat <strong>${t.seat}</strong></span>`:g}
          ${t.agentref?c`<code title=${t.agentref}>${t.agentref.slice(0,12)}</code>`:g}
          ${t.head?c`<a href=${"/provenance/"+t.head}><code>${t.head.slice(0,12)}</code></a>`:g}
          <a href=${"/items/"+t.id}>${t.id}</a>
        </div>
        ${""}
        ${t.layers?.length?c`<div class="layers">${t.layers.map(o=>c`
              <span class="layer" title=${`${o.source} ${o.digest}`}>${o.scope}:${o.name}</span>`)}</div>`:g}
        <h4>Timeline</h4>
        ${e.length===0?t.watching?c`<p class="empty">Started; nothing said yet.</p>`:c`<p class="blind">No live stream here — another runner holds this, or the window has rolled.
                     Silence about the run, not an idle agent.</p>`:c`<ol>${e.map(o=>c`
              <li class=${ji[o.kind]??""}>
                <span class="t">${new Date(o.at).toLocaleTimeString()}</span>
                <span class="k">${o.kind}</span>
                <span class="x">${yi(o)}</span>
              </li>`)}
            </ol>`}
      </jh-card>`}};customElements.define("box-sessions",so);var ao=class extends x{static styles=A`
    :host { display: block; }
    .empty { color: var(--jh-color-content-secondary-enabled); }
    .overdue { color: var(--jh-color-content-negative-enabled, #b3261e); font-weight: 600; }
    .told { font-size: .8rem; color: var(--jh-color-content-secondary-enabled); }
  `;_task=new E(this,{task:async(t,{signal:e})=>Do(e),args:()=>[]});render(){return c`
      <jh-card header-title="Exception queue" header-subtitle="Parked work waiting on a person" padding="medium">
        ${this._task.render({pending:()=>c`<jh-progress accessible-label="Loading exceptions"></jh-progress>`,error:t=>c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>(t?.length??0)===0?c`<p class="empty">Nothing here — the factory has nothing waiting on a person.</p>`:c`<jh-list-group accessible-label="Parked items">
                ${t.map(e=>c`
                  <jh-list-item primary-text=${e.id} secondary-text=${e.note}
                    secondary-metadata=${z(e.age_sec)+" parked"} show-divider>
                    <jh-tag slot="jh-list-item-left" size="small" label=${e.severity??"medium"}></jh-tag>
                    <div slot="jh-list-item-content">
                      ${e.overdue?c`<div class="overdue">past the response-time objective</div>`:g}
                      ${e.escalations?c`<div class="told">escalated out of band ${e.escalations}×</div>`:g}
                    </div>
                    <jh-button slot="jh-list-item-right" appearance="tertiary" size="small"
                      label="Open" href=${"/items/"+e.id}></jh-button>
                  </jh-list-item>`)}
              </jh-list-group>`})}
      </jh-card>`}};customElements.define("box-exceptions",ao);var no=class extends x{static properties={itemId:{attribute:!1}};static styles=A`
    :host { display: grid; gap: 16px; }
    pre { white-space: pre-wrap; word-break: break-all; font-size: .8rem;
          background: var(--jh-color-container-secondary-enabled); padding: 8px; border-radius: 6px; }
    .live { font-size: .75rem; color: var(--jh-color-content-secondary-enabled); margin-bottom: 8px; }
  `;constructor(){super(),this._activity=new Y(this,{limit:1,onEvent:t=>{t.scope===`workitem/${this.itemId}`&&this._refresh()}})}_task=new E(this,{task:async([t],{signal:e})=>{let[o,r]=await Promise.all([Mo(t,e),Io(t,e)]);return{item:o,evidence:r}},args:()=>[this.itemId]});_refresh(){clearTimeout(this._timer),this._timer=setTimeout(()=>this._task.run(),400)}disconnectedCallback(){super.disconnectedCallback(),clearTimeout(this._timer)}render(){return this._task.render({pending:()=>c`<jh-progress accessible-label="Loading item"></jh-progress>`,error:t=>c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:({item:t,evidence:e})=>c`
        <jh-card header-title=${t.id} header-subtitle=${t.task} padding="medium">
          <p><jh-tag size="small" label=${t.state}></jh-tag>
             ${t.head?c` <a href=${"/provenance/"+t.head}><code>${t.head.slice(0,12)}</code></a>`:g}</p>
          ${t.note?c`<pre>${t.note}</pre>`:g}
        </jh-card>
        <jh-card header-title="Timeline" header-subtitle="What this run did, in order" padding="medium">
          <div class="live">${this._activity.connected?"live":"reconnecting\u2026"}</div>
          <jh-list-group accessible-label="Run timeline">
            ${(e??[]).map(o=>c`
              <jh-list-item
                primary-text=${o.Kind}
                secondary-text=${vi(o)}
                primary-metadata=${o.At?new Date(o.At).toLocaleTimeString():""}
                secondary-metadata=${o.Actor}
                show-divider></jh-list-item>`)}
          </jh-list-group>
        </jh-card>`})}};customElements.define("box-item",no);var lo=class extends x{static properties={sha:{attribute:!1}};static styles=A`:host { display: block; }`;_task=new E(this,{task:async([t],{signal:e})=>t?Bo(t,e):[],args:()=>[this.sha]});render(){return c`
      <jh-card header-title="Provenance" header-subtitle="Everything recorded about one commit" padding="medium">
        ${this._task.render({pending:()=>c`<jh-progress accessible-label="Loading provenance"></jh-progress>`,error:t=>c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${t.message}</jh-notification>`,complete:t=>(t?.length??0)===0?c`<p>Nothing recorded about <code>${this.sha}</code>.</p>`:c`<jh-list-group accessible-label="Provenance records">
                ${t.map(e=>c`
                  <jh-list-item primary-text=${e.Kind} secondary-text=${e.Scope}
                    primary-metadata=${"seq "+e.Seq} secondary-metadata=${e.Actor} show-divider></jh-list-item>`)}
              </jh-list-group>`})}
      </jh-card>`}};customElements.define("box-provenance",lo);var ho=class extends x{static styles=A`
    :host { display: block; }
    form { display: grid; gap: 16px; max-width: 560px; }
    form .actions { padding-top: 8px; }
  `;static properties={_routes:{state:!0},_route:{state:!0},_key:{state:!0},_payload:{state:!0},_result:{state:!0},_error:{state:!0},_pending:{state:!0}};constructor(){super(),this._routes=[],this._route="",this._key="",this._payload="",this._result=null,this._error="",this._pending=!1}connectedCallback(){super.connectedCallback(),le().then(t=>{this._routes=t.routes,!this._route&&t.routes.length&&(this._route=t.routes[0])}).catch(t=>{this._error=t.message})}async _submit(t){t.preventDefault(),this._pending=!0,this._error="",this._result=null;try{this._result=await Wo(this._route,this._key,this._payload)}catch(e){this._error=e.message}finally{this._pending=!1}}render(){return c`
      <jh-card header-title="Submit work" header-subtitle="Open an ingress; the daemon's next tick takes it from there" padding="medium">
        <form @submit=${this._submit}>
          <jh-radio-group label="Route" name="route" .value=${this._route}
            helper-text="Routes are the operator's configuration; work inherits the route's target and authority"
            @jh-change=${t=>{this._route=t.target.value}}>
            ${this._routes.map(t=>c`<jh-radio label=${t} value=${t} ?checked=${t===this._route}></jh-radio>`)}
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
        ${this._error?c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${this._error}</jh-notification>`:g}
        ${this._result?c`
          <jh-notification appearance="positive" type="banner" hide-dismiss-button>
            ${this._result.opened?"Opened":"Already open"} — item
            <a href=${"/items/"+this._result.item}>${this._result.item}</a>
          </jh-notification>`:g}
      </jh-card>`}};customElements.define("box-submit",ho);Ho();var co=class extends x{static styles=A`
    :host { display: block; max-width: 960px; margin: 0 auto; padding: 16px; }
    header { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; margin-bottom: 16px; }
    h1 { font-size: 1.2rem; margin: 0; }
    nav { display: flex; gap: 4px; flex: 1; }
    nav a { text-decoration: none; padding: 6px 10px; border-radius: 6px;
            color: var(--jh-color-content-primary-enabled); font-size: .9rem; }
    nav a.active { background: var(--jh-color-container-secondary-enabled); font-weight: 600; }
    .controls { display: flex; gap: 8px; align-items: end; }
    main { display: block; }
  `;static properties={_reason:{state:!0},_controlError:{state:!0}};constructor(){super(),this._reason="",this._controlError=""}_router=new bt(this,[{path:"/",render:()=>c`<box-dashboard></box-dashboard>`},{path:"/work",render:()=>c`<box-work></box-work>`},{path:"/sessions",render:()=>c`<box-sessions></box-sessions>`},{path:"/exceptions",render:()=>c`<box-exceptions></box-exceptions>`},{path:"/items/:id",render:({id:t})=>c`<box-item .itemId=${t}></box-item>`},{path:"/provenance/:sha",render:({sha:t})=>c`<box-provenance .sha=${t}></box-provenance>`},{path:"/submit",render:()=>c`<box-submit></box-submit>`}],{fallback:{render:()=>c`<jh-notification appearance="neutral" type="banner" hide-dismiss-button>No such page.</jh-notification>`}});_active(t){return location.pathname===t?"active":""}async _control(t){this._controlError="";try{await(t==="stop"?Ko(this._reason):Fo(this._reason)),this._reason="",this.requestUpdate()}catch(e){this._controlError=e.message}}render(){return c`
      <header>
        <h1>Box Control Room</h1>
        <nav>
          <a href="/" class=${this._active("/")}>Dashboard</a>
          <a href="/work" class=${this._active("/work")}>Work</a>
          <a href="/sessions" class=${this._active("/sessions")}>Sessions</a>
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
      ${this._controlError?c`<jh-notification appearance="negative" type="banner" hide-dismiss-button>${this._controlError}</jh-notification>`:""}
      <main>${this._router.outlet()}</main>
    `}};customElements.define("box-app",co);export{co as BoxApp};
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
