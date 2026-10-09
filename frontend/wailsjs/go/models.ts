export namespace desktop {
	
	export class AccountProxyDTO {
	    mode: string;
	    scheme: string;
	    host: string;
	    port: number;
	    username: string;
	    password: string;
	    hasPassword: boolean;
	    contactsUseGlobal: boolean;
	    oauthUseGlobal: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AccountProxyDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.scheme = source["scheme"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.hasPassword = source["hasPassword"];
	        this.contactsUseGlobal = source["contactsUseGlobal"];
	        this.oauthUseGlobal = source["oauthUseGlobal"];
	    }
	}
	export class AccountDTO {
	    id: number;
	    email: string;
	    displayName: string;
	    localLabel: string;
	    useLocalLabel: boolean;
	    username: string;
	    imapHost: string;
	    imapPort: number;
	    smtpHost: string;
	    smtpPort: number;
	    local: boolean;
	    imapTls: string;
	    smtpTls: string;
	    exportOnArchive: boolean;
	    exportDir: string;
	    exportSubfolders: string;
	    exportNameTemplate: string;
	    pgpDefault: string;
	    passwordPromptDismissed: boolean;
	    syncMaxParallel?: number;
	    trustedCerts: string[];
	    caSubjects: string[];
	    proxy: AccountProxyDTO;
	
	    static createFrom(source: any = {}) {
	        return new AccountDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.email = source["email"];
	        this.displayName = source["displayName"];
	        this.localLabel = source["localLabel"];
	        this.useLocalLabel = source["useLocalLabel"];
	        this.username = source["username"];
	        this.imapHost = source["imapHost"];
	        this.imapPort = source["imapPort"];
	        this.smtpHost = source["smtpHost"];
	        this.smtpPort = source["smtpPort"];
	        this.local = source["local"];
	        this.imapTls = source["imapTls"];
	        this.smtpTls = source["smtpTls"];
	        this.exportOnArchive = source["exportOnArchive"];
	        this.exportDir = source["exportDir"];
	        this.exportSubfolders = source["exportSubfolders"];
	        this.exportNameTemplate = source["exportNameTemplate"];
	        this.pgpDefault = source["pgpDefault"];
	        this.passwordPromptDismissed = source["passwordPromptDismissed"];
	        this.syncMaxParallel = source["syncMaxParallel"];
	        this.trustedCerts = source["trustedCerts"];
	        this.caSubjects = source["caSubjects"];
	        this.proxy = this.convertValues(source["proxy"], AccountProxyDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class AccountSignaturesDTO {
	    headerId: number;
	    footerId: number;
	
	    static createFrom(source: any = {}) {
	        return new AccountSignaturesDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.headerId = source["headerId"];
	        this.footerId = source["footerId"];
	    }
	}
	export class AccountSyncStateDTO {
	    accountId: number;
	    email: string;
	    lastOk: string;
	    failedAt: string;
	    reason: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new AccountSyncStateDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accountId = source["accountId"];
	        this.email = source["email"];
	        this.lastOk = source["lastOk"];
	        this.failedAt = source["failedAt"];
	        this.reason = source["reason"];
	        this.detail = source["detail"];
	    }
	}
	export class AddAccountRequest {
	    email: string;
	    displayName: string;
	    localLabel: string;
	    useLocalLabel: boolean;
	    username: string;
	    imapHost: string;
	    imapPort: number;
	    smtpHost: string;
	    smtpPort: number;
	    imapTls: string;
	    smtpTls: string;
	    password: string;
	    provider: string;
	    clientId: string;
	    clientSecret: string;
	    trustedCerts: string[];
	    caPem: string;
	    proxy: AccountProxyDTO;
	
	    static createFrom(source: any = {}) {
	        return new AddAccountRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.displayName = source["displayName"];
	        this.localLabel = source["localLabel"];
	        this.useLocalLabel = source["useLocalLabel"];
	        this.username = source["username"];
	        this.imapHost = source["imapHost"];
	        this.imapPort = source["imapPort"];
	        this.smtpHost = source["smtpHost"];
	        this.smtpPort = source["smtpPort"];
	        this.imapTls = source["imapTls"];
	        this.smtpTls = source["smtpTls"];
	        this.password = source["password"];
	        this.provider = source["provider"];
	        this.clientId = source["clientId"];
	        this.clientSecret = source["clientSecret"];
	        this.trustedCerts = source["trustedCerts"];
	        this.caPem = source["caPem"];
	        this.proxy = this.convertValues(source["proxy"], AccountProxyDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AddressBookDTO {
	    id: number;
	    accountId: number;
	    name: string;
	    url: string;
	    collectionPath: string;
	    username: string;
	    readOnly: boolean;
	    lastSync: string;
	    lastError: string;
	    contactCount: number;
	    hasPassword: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AddressBookDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.accountId = source["accountId"];
	        this.name = source["name"];
	        this.url = source["url"];
	        this.collectionPath = source["collectionPath"];
	        this.username = source["username"];
	        this.readOnly = source["readOnly"];
	        this.lastSync = source["lastSync"];
	        this.lastError = source["lastError"];
	        this.contactCount = source["contactCount"];
	        this.hasPassword = source["hasPassword"];
	    }
	}
	export class AddressBookEntryDTO {
	    email: string;
	    name: string;
	    useCount: number;
	    lastUsed: string;
	    createdAt: string;
	    contact: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AddressBookEntryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.name = source["name"];
	        this.useCount = source["useCount"];
	        this.lastUsed = source["lastUsed"];
	        this.createdAt = source["createdAt"];
	        this.contact = source["contact"];
	    }
	}
	export class AddressBookRequest {
	    id: number;
	    accountId: number;
	    name: string;
	    url: string;
	    collectionPath: string;
	    username: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new AddressBookRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.accountId = source["accountId"];
	        this.name = source["name"];
	        this.url = source["url"];
	        this.collectionPath = source["collectionPath"];
	        this.username = source["username"];
	        this.password = source["password"];
	    }
	}
	export class AddressDTO {
	    name: string;
	    email: string;
	
	    static createFrom(source: any = {}) {
	        return new AddressDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.email = source["email"];
	    }
	}
	export class AgentActionDTO {
	    id: number;
	    tool: string;
	    summary: string;
	    error: string;
	    when: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentActionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tool = source["tool"];
	        this.summary = source["summary"];
	        this.error = source["error"];
	        this.when = source["when"];
	    }
	}
	export class AgentProposalDTO {
	    id: number;
	    accountId: number;
	    to: string;
	    cc: string;
	    bcc: string;
	    subject: string;
	    body: string;
	    when: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentProposalDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.accountId = source["accountId"];
	        this.to = source["to"];
	        this.cc = source["cc"];
	        this.bcc = source["bcc"];
	        this.subject = source["subject"];
	        this.body = source["body"];
	        this.when = source["when"];
	    }
	}
	export class ArchiveUndoDTO {
	    messageId: string;
	    originalFolderId: number;
	    destFolderId: number;
	    exportPath: string;
	    exportError: string;
	
	    static createFrom(source: any = {}) {
	        return new ArchiveUndoDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messageId = source["messageId"];
	        this.originalFolderId = source["originalFolderId"];
	        this.destFolderId = source["destFolderId"];
	        this.exportPath = source["exportPath"];
	        this.exportError = source["exportError"];
	    }
	}
	export class AttachmentContentDTO {
	    filename: string;
	    contentType: string;
	    sizeBytes: number;
	    data: string;
	    tooLarge: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AttachmentContentDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filename = source["filename"];
	        this.contentType = source["contentType"];
	        this.sizeBytes = source["sizeBytes"];
	        this.data = source["data"];
	        this.tooLarge = source["tooLarge"];
	    }
	}
	export class AttachmentDTO {
	    id: number;
	    filename: string;
	    contentType: string;
	    sizeBytes: number;
	    inline: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AttachmentDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.filename = source["filename"];
	        this.contentType = source["contentType"];
	        this.sizeBytes = source["sizeBytes"];
	        this.inline = source["inline"];
	    }
	}
	export class VerdictDTO {
	    status: string;
	    malicious: number;
	    suspicious: number;
	    total: number;
	    permalink: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new VerdictDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.malicious = source["malicious"];
	        this.suspicious = source["suspicious"];
	        this.total = source["total"];
	        this.permalink = source["permalink"];
	        this.error = source["error"];
	    }
	}
	export class AttachmentVerdictDTO {
	    attachmentId: number;
	    filename: string;
	    verdict: VerdictDTO;
	
	    static createFrom(source: any = {}) {
	        return new AttachmentVerdictDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attachmentId = source["attachmentId"];
	        this.filename = source["filename"];
	        this.verdict = this.convertValues(source["verdict"], VerdictDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BackupInfoDTO {
	    path: string;
	    createdAt: string;
	    appVersion: string;
	    hasSettings: boolean;
	    hasWhitelist: boolean;
	    hasMailboxes: boolean;
	    hasSignatures: boolean;
	    hasEncryptedCredentials: boolean;
	    settingCount: number;
	    mailboxCount: number;
	    signatureCount: number;
	
	    static createFrom(source: any = {}) {
	        return new BackupInfoDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.createdAt = source["createdAt"];
	        this.appVersion = source["appVersion"];
	        this.hasSettings = source["hasSettings"];
	        this.hasWhitelist = source["hasWhitelist"];
	        this.hasMailboxes = source["hasMailboxes"];
	        this.hasSignatures = source["hasSignatures"];
	        this.hasEncryptedCredentials = source["hasEncryptedCredentials"];
	        this.settingCount = source["settingCount"];
	        this.mailboxCount = source["mailboxCount"];
	        this.signatureCount = source["signatureCount"];
	    }
	}
	export class CAFileDTO {
	    pem: string;
	    subjects: string[];
	
	    static createFrom(source: any = {}) {
	        return new CAFileDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pem = source["pem"];
	        this.subjects = source["subjects"];
	    }
	}
	export class ComposeAttachment {
	    filename: string;
	    contentType: string;
	    contentBase64: string;
	    inline: boolean;
	    contentId: string;
	
	    static createFrom(source: any = {}) {
	        return new ComposeAttachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filename = source["filename"];
	        this.contentType = source["contentType"];
	        this.contentBase64 = source["contentBase64"];
	        this.inline = source["inline"];
	        this.contentId = source["contentId"];
	    }
	}
	export class ComposeRequest {
	    accountId: number;
	    to: AddressDTO[];
	    cc: AddressDTO[];
	    bcc: AddressDTO[];
	    subject: string;
	    text: string;
	    html: string;
	    inReplyTo: string;
	    references: string[];
	    attachments: ComposeAttachment[];
	    sendAt: string;
	    protection: string;
	
	    static createFrom(source: any = {}) {
	        return new ComposeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accountId = source["accountId"];
	        this.to = this.convertValues(source["to"], AddressDTO);
	        this.cc = this.convertValues(source["cc"], AddressDTO);
	        this.bcc = this.convertValues(source["bcc"], AddressDTO);
	        this.subject = source["subject"];
	        this.text = source["text"];
	        this.html = source["html"];
	        this.inReplyTo = source["inReplyTo"];
	        this.references = source["references"];
	        this.attachments = this.convertValues(source["attachments"], ComposeAttachment);
	        this.sendAt = source["sendAt"];
	        this.protection = source["protection"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UntrustedCertDTO {
	    server: string;
	    host: string;
	    port: number;
	    fingerprint: string;
	    display: string;
	    subject: string;
	    issuer: string;
	    notBefore: string;
	    notAfter: string;
	    names: string[];
	    selfSigned: boolean;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new UntrustedCertDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = source["server"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.fingerprint = source["fingerprint"];
	        this.display = source["display"];
	        this.subject = source["subject"];
	        this.issuer = source["issuer"];
	        this.notBefore = source["notBefore"];
	        this.notAfter = source["notAfter"];
	        this.names = source["names"];
	        this.selfSigned = source["selfSigned"];
	        this.reason = source["reason"];
	    }
	}
	export class ConnectionTestDTO {
	    untrusted: UntrustedCertDTO[];
	
	    static createFrom(source: any = {}) {
	        return new ConnectionTestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.untrusted = this.convertValues(source["untrusted"], UntrustedCertDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ContactValueDTO {
	    value: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new ContactValueDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.label = source["label"];
	    }
	}
	export class ContactDTO {
	    id: number;
	    bookId: number;
	    bookName: string;
	    uid: string;
	    fullName: string;
	    organization: string;
	    title: string;
	    note: string;
	    emails: ContactValueDTO[];
	    phones: ContactValueDTO[];
	    readOnly: boolean;
	    updated: string;
	    extra: string[];
	
	    static createFrom(source: any = {}) {
	        return new ContactDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.bookId = source["bookId"];
	        this.bookName = source["bookName"];
	        this.uid = source["uid"];
	        this.fullName = source["fullName"];
	        this.organization = source["organization"];
	        this.title = source["title"];
	        this.note = source["note"];
	        this.emails = this.convertValues(source["emails"], ContactValueDTO);
	        this.phones = this.convertValues(source["phones"], ContactValueDTO);
	        this.readOnly = source["readOnly"];
	        this.updated = source["updated"];
	        this.extra = source["extra"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ContactConflictDTO {
	    conflict: boolean;
	    server: ContactDTO;
	    mine: ContactDTO;
	    saved: ContactDTO;
	
	    static createFrom(source: any = {}) {
	        return new ContactConflictDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.conflict = source["conflict"];
	        this.server = this.convertValues(source["server"], ContactDTO);
	        this.mine = this.convertValues(source["mine"], ContactDTO);
	        this.saved = this.convertValues(source["saved"], ContactDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ContactRequest {
	    id: number;
	    bookId: number;
	    fullName: string;
	    organization: string;
	    title: string;
	    note: string;
	    emails: ContactValueDTO[];
	    phones: ContactValueDTO[];
	    force: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ContactRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.bookId = source["bookId"];
	        this.fullName = source["fullName"];
	        this.organization = source["organization"];
	        this.title = source["title"];
	        this.note = source["note"];
	        this.emails = this.convertValues(source["emails"], ContactValueDTO);
	        this.phones = this.convertValues(source["phones"], ContactValueDTO);
	        this.force = source["force"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class CreateFolderRequest {
	    accountId: number;
	    parentId: number;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateFolderRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accountId = source["accountId"];
	        this.parentId = source["parentId"];
	        this.name = source["name"];
	    }
	}
	export class DefaultMailStatusDTO {
	    known: boolean;
	    isDefault: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DefaultMailStatusDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.known = source["known"];
	        this.isDefault = source["isDefault"];
	    }
	}
	export class DevLogLineDTO {
	    seq: number;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new DevLogLineDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.text = source["text"];
	    }
	}
	export class DevActivityDTO {
	    lines: DevLogLineDTO[];
	    next: number;
	    level: string;
	
	    static createFrom(source: any = {}) {
	        return new DevActivityDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lines = this.convertValues(source["lines"], DevLogLineDTO);
	        this.next = source["next"];
	        this.level = source["level"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class DevProcessDTO {
	    goroutines: number;
	    heapBytes: number;
	    heapSysBytes: number;
	    gcRuns: number;
	    databaseBytes: number;
	    attachmentsBytes: number;
	    dataDirBytes: number;
	    uptimeSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new DevProcessDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.goroutines = source["goroutines"];
	        this.heapBytes = source["heapBytes"];
	        this.heapSysBytes = source["heapSysBytes"];
	        this.gcRuns = source["gcRuns"];
	        this.databaseBytes = source["databaseBytes"];
	        this.attachmentsBytes = source["attachmentsBytes"];
	        this.dataDirBytes = source["dataDirBytes"];
	        this.uptimeSeconds = source["uptimeSeconds"];
	    }
	}
	export class DiscoveredBookDTO {
	    name: string;
	    url: string;
	    collectionPath: string;
	    exists: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DiscoveredBookDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.url = source["url"];
	        this.collectionPath = source["collectionPath"];
	        this.exists = source["exists"];
	    }
	}
	export class DiscoveredDTO {
	    imapHost: string;
	    imapPort: number;
	    smtpHost: string;
	    smtpPort: number;
	    imapTls: string;
	    smtpTls: string;
	    oauth: boolean;
	    oauthProvider: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new DiscoveredDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.imapHost = source["imapHost"];
	        this.imapPort = source["imapPort"];
	        this.smtpHost = source["smtpHost"];
	        this.smtpPort = source["smtpPort"];
	        this.imapTls = source["imapTls"];
	        this.smtpTls = source["smtpTls"];
	        this.oauth = source["oauth"];
	        this.oauthProvider = source["oauthProvider"];
	        this.source = source["source"];
	    }
	}
	export class DraftDTO {
	    id: number;
	    savedAt: string;
	    request: ComposeRequest;
	    locked: boolean;
	    accountId: number;
	    protection: string;
	
	    static createFrom(source: any = {}) {
	        return new DraftDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.savedAt = source["savedAt"];
	        this.request = this.convertValues(source["request"], ComposeRequest);
	        this.locked = source["locked"];
	        this.accountId = source["accountId"];
	        this.protection = source["protection"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FetchOlderResult {
	    fetched: number;
	    hasOlder: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FetchOlderResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fetched = source["fetched"];
	        this.hasOlder = source["hasOlder"];
	    }
	}
	export class FolderDTO {
	    id: number;
	    accountId: number;
	    name: string;
	    imapPath: string;
	    delimiter: string;
	    parentId?: number;
	    role: string;
	    unreadCount: number;
	    totalCount: number;
	    attributes: string[];
	    pinned: boolean;
	    roleOverride: string;
	    syncExcluded: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FolderDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.accountId = source["accountId"];
	        this.name = source["name"];
	        this.imapPath = source["imapPath"];
	        this.delimiter = source["delimiter"];
	        this.parentId = source["parentId"];
	        this.role = source["role"];
	        this.unreadCount = source["unreadCount"];
	        this.totalCount = source["totalCount"];
	        this.attributes = source["attributes"];
	        this.pinned = source["pinned"];
	        this.roleOverride = source["roleOverride"];
	        this.syncExcluded = source["syncExcluded"];
	    }
	}
	export class ImageAllowEntryDTO {
	    value: string;
	    kind: string;
	    exampleMessageId: number;
	    exampleSubject: string;
	    exampleFrom: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageAllowEntryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.kind = source["kind"];
	        this.exampleMessageId = source["exampleMessageId"];
	        this.exampleSubject = source["exampleSubject"];
	        this.exampleFrom = source["exampleFrom"];
	    }
	}
	export class LinkVerdictDTO {
	    url: string;
	    verdict: VerdictDTO;
	
	    static createFrom(source: any = {}) {
	        return new LinkVerdictDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.verdict = this.convertValues(source["verdict"], VerdictDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ListMessagesRequest {
	    kind: string;
	    folderId: number;
	    view: string;
	    viewId: number;
	    limit: number;
	    offset: number;
	
	    static createFrom(source: any = {}) {
	        return new ListMessagesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.folderId = source["folderId"];
	        this.view = source["view"];
	        this.viewId = source["viewId"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	    }
	}
	export class LogStatusDTO {
	    dir: string;
	    writing: boolean;
	    forced: boolean;
	    sizeBytes: number;
	    crashName: string;
	    crashTime: string;
	
	    static createFrom(source: any = {}) {
	        return new LogStatusDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.writing = source["writing"];
	        this.forced = source["forced"];
	        this.sizeBytes = source["sizeBytes"];
	        this.crashName = source["crashName"];
	        this.crashTime = source["crashTime"];
	    }
	}
	export class MCPConfigDTO {
	    enabled: boolean;
	    port: number;
	    token: string;
	    url: string;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MCPConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	        this.token = source["token"];
	        this.url = source["url"];
	        this.running = source["running"];
	    }
	}
	export class MCPPermissionDTO {
	    tool: string;
	    group: string;
	    allowed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MCPPermissionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tool = source["tool"];
	        this.group = source["group"];
	        this.allowed = source["allowed"];
	    }
	}
	export class MailtoDraft {
	    to: string;
	    cc: string;
	    bcc: string;
	    subject: string;
	    body: string;
	
	    static createFrom(source: any = {}) {
	        return new MailtoDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.to = source["to"];
	        this.cc = source["cc"];
	        this.bcc = source["bcc"];
	        this.subject = source["subject"];
	        this.body = source["body"];
	    }
	}
	export class UnsubscribeDTO {
	    kind: string;
	    target: string;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UnsubscribeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.target = source["target"];
	        this.done = source["done"];
	    }
	}
	export class PhishingSignalDTO {
	    kind: string;
	    detail?: string;
	    target?: string;
	
	    static createFrom(source: any = {}) {
	        return new PhishingSignalDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.detail = source["detail"];
	        this.target = source["target"];
	    }
	}
	export class PhishingDTO {
	    level: string;
	    signals?: PhishingSignalDTO[];
	    links?: string[];
	
	    static createFrom(source: any = {}) {
	        return new PhishingDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.signals = this.convertValues(source["signals"], PhishingSignalDTO);
	        this.links = source["links"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TrackingPixelDTO {
	    host: string;
	    url: string;
	    reasons: string[];
	
	    static createFrom(source: any = {}) {
	        return new TrackingPixelDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.url = source["url"];
	        this.reasons = source["reasons"];
	    }
	}
	export class SMIMEDTO {
	    status: string;
	    signer: string;
	    email: string;
	    issuer: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new SMIMEDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.signer = source["signer"];
	        this.email = source["email"];
	        this.issuer = source["issuer"];
	        this.detail = source["detail"];
	    }
	}
	export class MessageDetailDTO {
	    id: number;
	    accountId: number;
	    folderId: number;
	    accountEmail: string;
	    folderName: string;
	    subject: string;
	    fromName: string;
	    fromAddress: string;
	    snippet: string;
	    date: string;
	    seen: boolean;
	    flagged: boolean;
	    hasAttachments: boolean;
	    pgp: string;
	    auth: string;
	    flagColor: number;
	    offline: boolean;
	    snoozeUntil: string;
	    senderVip: boolean;
	    smime: SMIMEDTO;
	    toAddresses: string;
	    ccAddresses: string;
	    bodyPlain: string;
	    bodyHtmlSafe: string;
	    bodyQuote: string;
	    isHtml: boolean;
	    hasRemoteContent: boolean;
	    remoteAllowed: boolean;
	    remoteHosts: string[];
	    trackingPixels: TrackingPixelDTO[];
	    attachments: AttachmentDTO[];
	    phishing: PhishingDTO;
	    pgpState: string;
	    unsubscribe?: UnsubscribeDTO;
	    charsetGuess: string;
	    bodyComplete: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MessageDetailDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.accountId = source["accountId"];
	        this.folderId = source["folderId"];
	        this.accountEmail = source["accountEmail"];
	        this.folderName = source["folderName"];
	        this.subject = source["subject"];
	        this.fromName = source["fromName"];
	        this.fromAddress = source["fromAddress"];
	        this.snippet = source["snippet"];
	        this.date = source["date"];
	        this.seen = source["seen"];
	        this.flagged = source["flagged"];
	        this.hasAttachments = source["hasAttachments"];
	        this.pgp = source["pgp"];
	        this.auth = source["auth"];
	        this.flagColor = source["flagColor"];
	        this.offline = source["offline"];
	        this.snoozeUntil = source["snoozeUntil"];
	        this.senderVip = source["senderVip"];
	        this.smime = this.convertValues(source["smime"], SMIMEDTO);
	        this.toAddresses = source["toAddresses"];
	        this.ccAddresses = source["ccAddresses"];
	        this.bodyPlain = source["bodyPlain"];
	        this.bodyHtmlSafe = source["bodyHtmlSafe"];
	        this.bodyQuote = source["bodyQuote"];
	        this.isHtml = source["isHtml"];
	        this.hasRemoteContent = source["hasRemoteContent"];
	        this.remoteAllowed = source["remoteAllowed"];
	        this.remoteHosts = source["remoteHosts"];
	        this.trackingPixels = this.convertValues(source["trackingPixels"], TrackingPixelDTO);
	        this.attachments = this.convertValues(source["attachments"], AttachmentDTO);
	        this.phishing = this.convertValues(source["phishing"], PhishingDTO);
	        this.pgpState = source["pgpState"];
	        this.unsubscribe = this.convertValues(source["unsubscribe"], UnsubscribeDTO);
	        this.charsetGuess = source["charsetGuess"];
	        this.bodyComplete = source["bodyComplete"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MessageIDsDTO {
	    ids: number[];
	    capped: boolean;
	    matching: number;
	
	    static createFrom(source: any = {}) {
	        return new MessageIDsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ids = source["ids"];
	        this.capped = source["capped"];
	        this.matching = source["matching"];
	    }
	}
	export class MessageSummaryDTO {
	    id: number;
	    accountId: number;
	    folderId: number;
	    accountEmail: string;
	    folderName: string;
	    subject: string;
	    fromName: string;
	    fromAddress: string;
	    snippet: string;
	    date: string;
	    seen: boolean;
	    flagged: boolean;
	    hasAttachments: boolean;
	    pgp: string;
	    auth: string;
	    flagColor: number;
	    offline: boolean;
	    snoozeUntil: string;
	    senderVip: boolean;
	    smime: SMIMEDTO;
	
	    static createFrom(source: any = {}) {
	        return new MessageSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.accountId = source["accountId"];
	        this.folderId = source["folderId"];
	        this.accountEmail = source["accountEmail"];
	        this.folderName = source["folderName"];
	        this.subject = source["subject"];
	        this.fromName = source["fromName"];
	        this.fromAddress = source["fromAddress"];
	        this.snippet = source["snippet"];
	        this.date = source["date"];
	        this.seen = source["seen"];
	        this.flagged = source["flagged"];
	        this.hasAttachments = source["hasAttachments"];
	        this.pgp = source["pgp"];
	        this.auth = source["auth"];
	        this.flagColor = source["flagColor"];
	        this.offline = source["offline"];
	        this.snoozeUntil = source["snoozeUntil"];
	        this.senderVip = source["senderVip"];
	        this.smime = this.convertValues(source["smime"], SMIMEDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MessageListDTO {
	    messages: MessageSummaryDTO[];
	    total: number;
	    hasOlder: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MessageListDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messages = this.convertValues(source["messages"], MessageSummaryDTO);
	        this.total = source["total"];
	        this.hasOlder = source["hasOlder"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MessageScanDTO {
	    links: LinkVerdictDTO[];
	    attachments: AttachmentVerdictDTO[];
	
	    static createFrom(source: any = {}) {
	        return new MessageScanDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.links = this.convertValues(source["links"], LinkVerdictDTO);
	        this.attachments = this.convertValues(source["attachments"], AttachmentVerdictDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class OutboxRowDTO {
	    id: number;
	    accountId: number;
	    recipients: string[];
	    state: string;
	    attempts: number;
	    lastError: string;
	    nextAttemptAt: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new OutboxRowDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.accountId = source["accountId"];
	        this.recipients = source["recipients"];
	        this.state = source["state"];
	        this.attempts = source["attempts"];
	        this.lastError = source["lastError"];
	        this.nextAttemptAt = source["nextAttemptAt"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class PGPKeyDTO {
	    fingerprint: string;
	    name: string;
	    email: string;
	    emails: string[];
	    created: string;
	    expires: string;
	    expired: boolean;
	    hasPrivate: boolean;
	    locked: boolean;
	    unlocked: boolean;
	    remembered: boolean;
	    algorithm: string;
	    bits: number;
	
	    static createFrom(source: any = {}) {
	        return new PGPKeyDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fingerprint = source["fingerprint"];
	        this.name = source["name"];
	        this.email = source["email"];
	        this.emails = source["emails"];
	        this.created = source["created"];
	        this.expires = source["expires"];
	        this.expired = source["expired"];
	        this.hasPrivate = source["hasPrivate"];
	        this.locked = source["locked"];
	        this.unlocked = source["unlocked"];
	        this.remembered = source["remembered"];
	        this.algorithm = source["algorithm"];
	        this.bits = source["bits"];
	    }
	}
	export class PasswordCheckDTO {
	    ok: boolean;
	    rejected: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new PasswordCheckDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.rejected = source["rejected"];
	        this.error = source["error"];
	    }
	}
	export class PendingMailtoDTO {
	    present: boolean;
	    draft: MailtoDraft;
	
	    static createFrom(source: any = {}) {
	        return new PendingMailtoDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.present = source["present"];
	        this.draft = this.convertValues(source["draft"], MailtoDraft);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class ProfileDTO {
	    id: number;
	    name: string;
	    icon: string;
	    main: boolean;
	    active: boolean;
	    shareSettings: boolean;
	    shareSignatures: boolean;
	    shareViews: boolean;
	    shareLayout: boolean;
	    accountIds: number[];
	
	    static createFrom(source: any = {}) {
	        return new ProfileDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.icon = source["icon"];
	        this.main = source["main"];
	        this.active = source["active"];
	        this.shareSettings = source["shareSettings"];
	        this.shareSignatures = source["shareSignatures"];
	        this.shareViews = source["shareViews"];
	        this.shareLayout = source["shareLayout"];
	        this.accountIds = source["accountIds"];
	    }
	}
	export class ProfileRequest {
	    id: number;
	    name: string;
	    icon: string;
	    accountIds: number[];
	    startSettings: string;
	    startSignatures: string;
	    startViews: string;
	    startLayout: string;
	
	    static createFrom(source: any = {}) {
	        return new ProfileRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.icon = source["icon"];
	        this.accountIds = source["accountIds"];
	        this.startSettings = source["startSettings"];
	        this.startSignatures = source["startSignatures"];
	        this.startViews = source["startViews"];
	        this.startLayout = source["startLayout"];
	    }
	}
	export class RecipientKeyDTO {
	    email: string;
	    hasKey: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RecipientKeyDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.hasKey = source["hasKey"];
	    }
	}
	export class ProtectionStatusDTO {
	    canSign: boolean;
	    signerLocked: boolean;
	    canEncrypt: boolean;
	    recipients: RecipientKeyDTO[];
	    default: string;
	    suggested: string;
	
	    static createFrom(source: any = {}) {
	        return new ProtectionStatusDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canSign = source["canSign"];
	        this.signerLocked = source["signerLocked"];
	        this.canEncrypt = source["canEncrypt"];
	        this.recipients = this.convertValues(source["recipients"], RecipientKeyDTO);
	        this.default = source["default"];
	        this.suggested = source["suggested"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProxyConfigDTO {
	    mode: string;
	    scheme: string;
	    host: string;
	    port: number;
	    username: string;
	    password: string;
	    hasPassword: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProxyConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.scheme = source["scheme"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.hasPassword = source["hasPassword"];
	    }
	}
	
	export class RouteTestRequest {
	    accountId: number;
	    proxy: AccountProxyDTO;
	    imapHost: string;
	    imapPort: number;
	    smtpHost: string;
	    smtpPort: number;
	
	    static createFrom(source: any = {}) {
	        return new RouteTestRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accountId = source["accountId"];
	        this.proxy = this.convertValues(source["proxy"], AccountProxyDTO);
	        this.imapHost = source["imapHost"];
	        this.imapPort = source["imapPort"];
	        this.smtpHost = source["smtpHost"];
	        this.smtpPort = source["smtpPort"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SMIMERevocationDTO {
	    status: string;
	    detail: string;
	    revokedAt: string;
	    checked: string;
	
	    static createFrom(source: any = {}) {
	        return new SMIMERevocationDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.detail = source["detail"];
	        this.revokedAt = source["revokedAt"];
	        this.checked = source["checked"];
	    }
	}
	export class SaveThemeRequest {
	    id: string;
	    name: string;
	    author: string;
	    version: string;
	    base: string;
	    tokens: Record<string, string>;
	    css: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveThemeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.author = source["author"];
	        this.version = source["version"];
	        this.base = source["base"];
	        this.tokens = source["tokens"];
	        this.css = source["css"];
	    }
	}
	export class SearchRequestDTO {
	    query: string;
	    afterUnix: number;
	    beforeUnix: number;
	    limit: number;
	    offset: number;
	    from: string;
	    to: string;
	    subject: string;
	    hasAttachment: boolean;
	    unreadOnly: boolean;
	    sort: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.query = source["query"];
	        this.afterUnix = source["afterUnix"];
	        this.beforeUnix = source["beforeUnix"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.subject = source["subject"];
	        this.hasAttachment = source["hasAttachment"];
	        this.unreadOnly = source["unreadOnly"];
	        this.sort = source["sort"];
	    }
	}
	export class SearchResultDTO {
	    messages: MessageSummaryDTO[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new SearchResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messages = this.convertValues(source["messages"], MessageSummaryDTO);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SettingResult {
	    value: string;
	    found: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SettingResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.found = source["found"];
	    }
	}
	export class SignatureDTO {
	    id: number;
	    name: string;
	    kind: string;
	    format: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new SignatureDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.format = source["format"];
	        this.content = source["content"];
	    }
	}
	export class TestConnectionRequest {
	    email: string;
	    username: string;
	    imapHost: string;
	    imapPort: number;
	    imapTls: string;
	    password: string;
	    smtpHost: string;
	    smtpPort: number;
	    smtpTls: string;
	    trustedCerts: string[];
	    caPem: string;
	    proxy: AccountProxyDTO;
	
	    static createFrom(source: any = {}) {
	        return new TestConnectionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.username = source["username"];
	        this.imapHost = source["imapHost"];
	        this.imapPort = source["imapPort"];
	        this.imapTls = source["imapTls"];
	        this.password = source["password"];
	        this.smtpHost = source["smtpHost"];
	        this.smtpPort = source["smtpPort"];
	        this.smtpTls = source["smtpTls"];
	        this.trustedCerts = source["trustedCerts"];
	        this.caPem = source["caPem"];
	        this.proxy = this.convertValues(source["proxy"], AccountProxyDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ThemeApplyDTO {
	    id: string;
	    base: string;
	    tokens: Record<string, string>;
	    css: string;
	    icons: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ThemeApplyDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.base = source["base"];
	        this.tokens = source["tokens"];
	        this.css = source["css"];
	        this.icons = source["icons"];
	    }
	}
	export class ThemeDraftDTO {
	    id: string;
	    name: string;
	    author: string;
	    version: string;
	    base: string;
	    tokens: Record<string, string>;
	    css: string;
	
	    static createFrom(source: any = {}) {
	        return new ThemeDraftDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.author = source["author"];
	        this.version = source["version"];
	        this.base = source["base"];
	        this.tokens = source["tokens"];
	        this.css = source["css"];
	    }
	}
	export class ThemeInfoDTO {
	    id: string;
	    name: string;
	    author: string;
	    version: string;
	    description: string;
	    base: string;
	    hasCss: boolean;
	    remoteRefs: string[];
	    preview: string;
	    compatWarning: string;
	    swatches: string[];
	
	    static createFrom(source: any = {}) {
	        return new ThemeInfoDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.author = source["author"];
	        this.version = source["version"];
	        this.description = source["description"];
	        this.base = source["base"];
	        this.hasCss = source["hasCss"];
	        this.remoteRefs = source["remoteRefs"];
	        this.preview = source["preview"];
	        this.compatWarning = source["compatWarning"];
	        this.swatches = source["swatches"];
	    }
	}
	export class ThemeImportPreviewDTO {
	    canceled: boolean;
	    path: string;
	    info: ThemeInfoDTO;
	    cssFiles: themepack.CSSFile[];
	    tokenCount: number;
	    updatesExisting: boolean;
	    installedVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new ThemeImportPreviewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canceled = source["canceled"];
	        this.path = source["path"];
	        this.info = this.convertValues(source["info"], ThemeInfoDTO);
	        this.cssFiles = this.convertValues(source["cssFiles"], themepack.CSSFile);
	        this.tokenCount = source["tokenCount"];
	        this.updatesExisting = source["updatesExisting"];
	        this.installedVersion = source["installedVersion"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ThunderbirdAccountDTO {
	    email: string;
	    displayName: string;
	    username: string;
	    imapHost: string;
	    imapPort: number;
	    smtpHost: string;
	    smtpPort: number;
	    kind: string;
	    exists: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ThunderbirdAccountDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.email = source["email"];
	        this.displayName = source["displayName"];
	        this.username = source["username"];
	        this.imapHost = source["imapHost"];
	        this.imapPort = source["imapPort"];
	        this.smtpHost = source["smtpHost"];
	        this.smtpPort = source["smtpPort"];
	        this.kind = source["kind"];
	        this.exists = source["exists"];
	    }
	}
	export class ThunderbirdFolderDTO {
	    name: string;
	    path: string;
	    sizeBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new ThunderbirdFolderDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.sizeBytes = source["sizeBytes"];
	    }
	}
	export class ThunderbirdProfileDTO {
	    name: string;
	    path: string;
	    accounts: ThunderbirdAccountDTO[];
	    localFolders: ThunderbirdFolderDTO[];
	
	    static createFrom(source: any = {}) {
	        return new ThunderbirdProfileDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.accounts = this.convertValues(source["accounts"], ThunderbirdAccountDTO);
	        this.localFolders = this.convertValues(source["localFolders"], ThunderbirdFolderDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class UIPrefsDTO {
	    theme: string;
	    accent: string;
	    density: string;
	    showMailboxBadge: boolean;
	    showDateTime: boolean;
	    showPgp: boolean;
	    showAuth: boolean;
	    toastPosition: string;
	    paneLocked: boolean;
	    sidebarWidth: number;
	    listWidth: number;
	    sendDelaySeconds: number;
	    flagHighlight: string;
	    showShortcutHints: boolean;
	    harvestAddresses: boolean;
	    showAccountEmail: boolean;
	    alwaysLoadImages: boolean;
	    blockTrackingPixels: boolean;
	    avatarSource: string;
	    avatarStyle: string;
	    multiSelectEnabled: boolean;
	    showSelectedCount: boolean;
	    selectAllScope: string;
	    selectAllUnified: boolean;
	    searchSortText: string;
	    searchSortDated: string;
	    searchSortFiltered: string;
	    sidebarIndentGuides: boolean;
	    rowTemplate: string;
	    rowShowAvatar: boolean;
	    rowShowSnippet: boolean;
	    previewLines: number;
	    uiScale: string;
	    messageFontSize: number;
	    viewsPlacement: string;
	    showFlaggedCount: boolean;
	    flagColorSync: boolean;
	    showOfflineIndicator: boolean;
	    showUnsyncedFolder: boolean;
	    paletteProfiles: boolean;
	    restoreTabs: boolean;
	    swipeEnabled: boolean;
	    swipeLeftAction: string;
	    swipeRightAction: string;
	    composeVimMode: boolean;
	    downloadIncludeAttachments: boolean;
	    appVimMode: boolean;
	    language: string;
	    lowPowerMode: boolean;
	    autoSyncIntervalSeconds: number;
	    defaultEditorMode: string;
	    composeAutocomplete: boolean;
	    composeChips: boolean;
	    updateCheckFrequency: string;
	    emptyStateImage: string;
	    emptyStateFullscreen: boolean;
	    cornerStyle: string;
	    themeId: string;
	    menuBarInApp: boolean;
	    menuBarNativeMinimal: boolean;
	    menuBarIcons: boolean;
	    timeFormat: string;
	    reduceMotion: boolean;
	    handCursor: boolean;
	    unreadBadge: boolean;
	    themeDarkStart: string;
	    themeDarkEnd: string;
	    bodyFont: string;
	    uiFont: string;
	    monoFont: string;
	    senderFonts: boolean;
	    notifyNewMail: boolean;
	    verboseSync: boolean;
	    syncProgressBar: boolean;
	    closeAction: string;
	    syncMessageLimit: number;
	    syncAutoBackfill: boolean;
	    syncMaxParallel: number;
	    syncFullReconcileDays: number;
	    startupSelection: string;
	    logToFile: boolean;
	    logLevel: string;
	    logMessageMetadata: boolean;
	    crashLogs: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UIPrefsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.accent = source["accent"];
	        this.density = source["density"];
	        this.showMailboxBadge = source["showMailboxBadge"];
	        this.showDateTime = source["showDateTime"];
	        this.showPgp = source["showPgp"];
	        this.showAuth = source["showAuth"];
	        this.toastPosition = source["toastPosition"];
	        this.paneLocked = source["paneLocked"];
	        this.sidebarWidth = source["sidebarWidth"];
	        this.listWidth = source["listWidth"];
	        this.sendDelaySeconds = source["sendDelaySeconds"];
	        this.flagHighlight = source["flagHighlight"];
	        this.showShortcutHints = source["showShortcutHints"];
	        this.harvestAddresses = source["harvestAddresses"];
	        this.showAccountEmail = source["showAccountEmail"];
	        this.alwaysLoadImages = source["alwaysLoadImages"];
	        this.blockTrackingPixels = source["blockTrackingPixels"];
	        this.avatarSource = source["avatarSource"];
	        this.avatarStyle = source["avatarStyle"];
	        this.multiSelectEnabled = source["multiSelectEnabled"];
	        this.showSelectedCount = source["showSelectedCount"];
	        this.selectAllScope = source["selectAllScope"];
	        this.selectAllUnified = source["selectAllUnified"];
	        this.searchSortText = source["searchSortText"];
	        this.searchSortDated = source["searchSortDated"];
	        this.searchSortFiltered = source["searchSortFiltered"];
	        this.sidebarIndentGuides = source["sidebarIndentGuides"];
	        this.rowTemplate = source["rowTemplate"];
	        this.rowShowAvatar = source["rowShowAvatar"];
	        this.rowShowSnippet = source["rowShowSnippet"];
	        this.previewLines = source["previewLines"];
	        this.uiScale = source["uiScale"];
	        this.messageFontSize = source["messageFontSize"];
	        this.viewsPlacement = source["viewsPlacement"];
	        this.showFlaggedCount = source["showFlaggedCount"];
	        this.flagColorSync = source["flagColorSync"];
	        this.showOfflineIndicator = source["showOfflineIndicator"];
	        this.showUnsyncedFolder = source["showUnsyncedFolder"];
	        this.paletteProfiles = source["paletteProfiles"];
	        this.restoreTabs = source["restoreTabs"];
	        this.swipeEnabled = source["swipeEnabled"];
	        this.swipeLeftAction = source["swipeLeftAction"];
	        this.swipeRightAction = source["swipeRightAction"];
	        this.composeVimMode = source["composeVimMode"];
	        this.downloadIncludeAttachments = source["downloadIncludeAttachments"];
	        this.appVimMode = source["appVimMode"];
	        this.language = source["language"];
	        this.lowPowerMode = source["lowPowerMode"];
	        this.autoSyncIntervalSeconds = source["autoSyncIntervalSeconds"];
	        this.defaultEditorMode = source["defaultEditorMode"];
	        this.composeAutocomplete = source["composeAutocomplete"];
	        this.composeChips = source["composeChips"];
	        this.updateCheckFrequency = source["updateCheckFrequency"];
	        this.emptyStateImage = source["emptyStateImage"];
	        this.emptyStateFullscreen = source["emptyStateFullscreen"];
	        this.cornerStyle = source["cornerStyle"];
	        this.themeId = source["themeId"];
	        this.menuBarInApp = source["menuBarInApp"];
	        this.menuBarNativeMinimal = source["menuBarNativeMinimal"];
	        this.menuBarIcons = source["menuBarIcons"];
	        this.timeFormat = source["timeFormat"];
	        this.reduceMotion = source["reduceMotion"];
	        this.handCursor = source["handCursor"];
	        this.unreadBadge = source["unreadBadge"];
	        this.themeDarkStart = source["themeDarkStart"];
	        this.themeDarkEnd = source["themeDarkEnd"];
	        this.bodyFont = source["bodyFont"];
	        this.uiFont = source["uiFont"];
	        this.monoFont = source["monoFont"];
	        this.senderFonts = source["senderFonts"];
	        this.notifyNewMail = source["notifyNewMail"];
	        this.verboseSync = source["verboseSync"];
	        this.syncProgressBar = source["syncProgressBar"];
	        this.closeAction = source["closeAction"];
	        this.syncMessageLimit = source["syncMessageLimit"];
	        this.syncAutoBackfill = source["syncAutoBackfill"];
	        this.syncMaxParallel = source["syncMaxParallel"];
	        this.syncFullReconcileDays = source["syncFullReconcileDays"];
	        this.startupSelection = source["startupSelection"];
	        this.logToFile = source["logToFile"];
	        this.logLevel = source["logLevel"];
	        this.logMessageMetadata = source["logMessageMetadata"];
	        this.crashLogs = source["crashLogs"];
	    }
	}
	export class UnifiedViewDTO {
	    key: string;
	    label: string;
	    unreadCount: number;
	    totalCount: number;
	
	    static createFrom(source: any = {}) {
	        return new UnifiedViewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.unreadCount = source["unreadCount"];
	        this.totalCount = source["totalCount"];
	    }
	}
	
	
	export class UpdateAccountRequest {
	    id: number;
	    displayName: string;
	    localLabel: string;
	    useLocalLabel: boolean;
	    username: string;
	    imapHost: string;
	    imapPort: number;
	    smtpHost: string;
	    smtpPort: number;
	    password: string;
	    imapTls: string;
	    smtpTls: string;
	    exportOnArchive: boolean;
	    exportDir: string;
	    exportSubfolders: string;
	    exportNameTemplate: string;
	    pgpDefault: string;
	    syncMaxParallel?: number;
	    proxy: AccountProxyDTO;
	
	    static createFrom(source: any = {}) {
	        return new UpdateAccountRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	        this.localLabel = source["localLabel"];
	        this.useLocalLabel = source["useLocalLabel"];
	        this.username = source["username"];
	        this.imapHost = source["imapHost"];
	        this.imapPort = source["imapPort"];
	        this.smtpHost = source["smtpHost"];
	        this.smtpPort = source["smtpPort"];
	        this.password = source["password"];
	        this.imapTls = source["imapTls"];
	        this.smtpTls = source["smtpTls"];
	        this.exportOnArchive = source["exportOnArchive"];
	        this.exportDir = source["exportDir"];
	        this.exportSubfolders = source["exportSubfolders"];
	        this.exportNameTemplate = source["exportNameTemplate"];
	        this.pgpDefault = source["pgpDefault"];
	        this.syncMaxParallel = source["syncMaxParallel"];
	        this.proxy = this.convertValues(source["proxy"], AccountProxyDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UpdateCheckResult {
	    checked: boolean;
	    available: boolean;
	    currentVersion: string;
	    latestVersion: string;
	    releaseUrl: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.checked = source["checked"];
	        this.available = source["available"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.releaseUrl = source["releaseUrl"];
	        this.error = source["error"];
	    }
	}
	export class UserLocaleApplyDTO {
	    id: string;
	    name: string;
	    base: string;
	    strings: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new UserLocaleApplyDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.base = source["base"];
	        this.strings = source["strings"];
	    }
	}
	export class UserLocaleDTO {
	    id: string;
	    name: string;
	    author: string;
	    base: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new UserLocaleDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.author = source["author"];
	        this.base = source["base"];
	        this.count = source["count"];
	    }
	}
	
	export class ViewDTO {
	    id: number;
	    name: string;
	    icon: string;
	    color: string;
	    queryText: string;
	    queryFrom: string[];
	    queryTo: string[];
	    querySubject: string;
	    withinDays: number;
	    useRegex: boolean;
	    unreadOnly: boolean;
	    flaggedOnly: boolean;
	    hasAttachment: boolean;
	    accountId: number;
	    position: number;
	    unreadCount: number;
	    totalCount: number;
	
	    static createFrom(source: any = {}) {
	        return new ViewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.icon = source["icon"];
	        this.color = source["color"];
	        this.queryText = source["queryText"];
	        this.queryFrom = source["queryFrom"];
	        this.queryTo = source["queryTo"];
	        this.querySubject = source["querySubject"];
	        this.withinDays = source["withinDays"];
	        this.useRegex = source["useRegex"];
	        this.unreadOnly = source["unreadOnly"];
	        this.flaggedOnly = source["flaggedOnly"];
	        this.hasAttachment = source["hasAttachment"];
	        this.accountId = source["accountId"];
	        this.position = source["position"];
	        this.unreadCount = source["unreadCount"];
	        this.totalCount = source["totalCount"];
	    }
	}
	export class VirusTotalConfigDTO {
	    enabled: boolean;
	    hasApiKey: boolean;
	    autoScanLinks: boolean;
	    autoScanAttachments: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VirusTotalConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.hasApiKey = source["hasApiKey"];
	        this.autoScanLinks = source["autoScanLinks"];
	        this.autoScanAttachments = source["autoScanAttachments"];
	    }
	}

}

export namespace themepack {
	
	export class CSSFile {
	    path: string;
	    content: string;
	    remoteRefs: string[];
	
	    static createFrom(source: any = {}) {
	        return new CSSFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.content = source["content"];
	        this.remoteRefs = source["remoteRefs"];
	    }
	}

}

