import { readApiError, type ApiError } from './apiError';

// saveDownload fetches a file the person asked for and hands it to the
// browser under `name`. It is an action with its own result, not a state the
// screen shows: it returns null when the file was handed over, and the
// server's refusal, or an empty error when no answer arrived, otherwise. It
// reads only and never throws.
// saveDownload, kişinin istediği dosyayı alır ve `name` adıyla tarayıcıya
// verir. Ekranın gösterdiği bir durum değil, kendi sonucu olan bir eylemdir:
// dosya verildiyse null, aksi hâlde sunucunun reddini ya da yanıt gelmediyse
// boş bir hatayı döndürür. Yalnız okur ve hata fırlatmaz.
export async function saveDownload(url: string, name: string): Promise<ApiError | null> {
    try {
        const res = await fetch(url);
        if (!res.ok) return await readApiError(res);
        const blobURL = URL.createObjectURL(await res.blob());
        const link = document.createElement('a');
        link.href = blobURL;
        link.download = name;
        document.body.appendChild(link);
        link.click();
        link.remove();
        URL.revokeObjectURL(blobURL);
        return null;
    } catch {
        return { message: '' };
    }
}
