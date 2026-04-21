export namespace models {
	
	export class Attach {
	    _id: string;
	    FileId: string;
	    ServerFileId?: string;
	    NoteId: string;
	    UserId: string;
	    Title?: string;
	    Type?: string;
	    Path?: string;
	    IsAttach: boolean;
	    IsDirty: boolean;
	    // Go type: time
	    CreatedTime?: any;
	
	    static createFrom(source: any = {}) {
	        return new Attach(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this._id = source["_id"];
	        this.FileId = source["FileId"];
	        this.ServerFileId = source["ServerFileId"];
	        this.NoteId = source["NoteId"];
	        this.UserId = source["UserId"];
	        this.Title = source["Title"];
	        this.Type = source["Type"];
	        this.Path = source["Path"];
	        this.IsAttach = source["IsAttach"];
	        this.IsDirty = source["IsDirty"];
	        this.CreatedTime = this.convertValues(source["CreatedTime"], null);
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
	export class FileRef {
	    FileId: string;
	    LocalFileId?: string;
	    ServerFileId?: string;
	    Type?: string;
	    HasBody: boolean;
	    IsAttach: boolean;
	    Title?: string;
	    Path?: string;
	    IsDirty: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.FileId = source["FileId"];
	        this.LocalFileId = source["LocalFileId"];
	        this.ServerFileId = source["ServerFileId"];
	        this.Type = source["Type"];
	        this.HasBody = source["HasBody"];
	        this.IsAttach = source["IsAttach"];
	        this.Title = source["Title"];
	        this.Path = source["Path"];
	        this.IsDirty = source["IsDirty"];
	    }
	}
	export class Note {
	    _id: string;
	    NoteId: string;
	    ServerNoteId?: string;
	    NotebookId: string;
	    UserId: string;
	    Title?: string;
	    Content?: string;
	    Desc?: string;
	    Abstract?: string;
	    ImgSrc?: string;
	    Tags?: string[];
	    IsMarkdown: boolean;
	    IsTrash: boolean;
	    IsBlog: boolean;
	    IsStar: boolean;
	    IsDeleted?: boolean;
	    IsNew?: boolean;
	    Usn: number;
	    IsDirty: boolean;
	    ContentIsDirty: boolean;
	    LocalIsNew: boolean;
	    LocalIsDelete: boolean;
	    InitSync: boolean;
	    ConflictNoteId?: string;
	    // Go type: time
	    ConflictTime?: any;
	    ConflictFixed: boolean;
	    Err?: string;
	    LocalContent?: string;
	    // Go type: time
	    CreatedTime?: any;
	    // Go type: time
	    UpdatedTime?: any;
	    Attachs?: Attach[];
	    Files?: FileRef[];
	    FileDatas?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new Note(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this._id = source["_id"];
	        this.NoteId = source["NoteId"];
	        this.ServerNoteId = source["ServerNoteId"];
	        this.NotebookId = source["NotebookId"];
	        this.UserId = source["UserId"];
	        this.Title = source["Title"];
	        this.Content = source["Content"];
	        this.Desc = source["Desc"];
	        this.Abstract = source["Abstract"];
	        this.ImgSrc = source["ImgSrc"];
	        this.Tags = source["Tags"];
	        this.IsMarkdown = source["IsMarkdown"];
	        this.IsTrash = source["IsTrash"];
	        this.IsBlog = source["IsBlog"];
	        this.IsStar = source["IsStar"];
	        this.IsDeleted = source["IsDeleted"];
	        this.IsNew = source["IsNew"];
	        this.Usn = source["Usn"];
	        this.IsDirty = source["IsDirty"];
	        this.ContentIsDirty = source["ContentIsDirty"];
	        this.LocalIsNew = source["LocalIsNew"];
	        this.LocalIsDelete = source["LocalIsDelete"];
	        this.InitSync = source["InitSync"];
	        this.ConflictNoteId = source["ConflictNoteId"];
	        this.ConflictTime = this.convertValues(source["ConflictTime"], null);
	        this.ConflictFixed = source["ConflictFixed"];
	        this.Err = source["Err"];
	        this.LocalContent = source["LocalContent"];
	        this.CreatedTime = this.convertValues(source["CreatedTime"], null);
	        this.UpdatedTime = this.convertValues(source["UpdatedTime"], null);
	        this.Attachs = this.convertValues(source["Attachs"], Attach);
	        this.Files = this.convertValues(source["Files"], FileRef);
	        this.FileDatas = source["FileDatas"];
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
	export class SyncConflict {
	    server?: Note;
	    local?: Note;
	    conflict_copy?: Note;
	
	    static createFrom(source: any = {}) {
	        return new SyncConflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = this.convertValues(source["server"], Note);
	        this.local = this.convertValues(source["local"], Note);
	        this.conflict_copy = this.convertValues(source["conflict_copy"], Note);
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
	export class SyncError {
	    err: string;
	    ret: string;
	    note?: Note;
	
	    static createFrom(source: any = {}) {
	        return new SyncError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.err = source["err"];
	        this.ret = source["ret"];
	        this.note = this.convertValues(source["note"], Note);
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

}

