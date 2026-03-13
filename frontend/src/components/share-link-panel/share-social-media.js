/* eslint-disable */
import React from 'react';
import copy from 'copy-to-clipboard';
import toaster from '../toast';
import { gettext } from '../../utils/constants';
import { Button, FormGroup, InputGroupAddon } from 'reactstrap';
import { changeLinkToChina } from '../../services/links';

const MoreShareButton = ({ url, text, title }) => {
    const handleShare = async (e) => {
        e.preventDefault()
        if (navigator.share) {
            try {
                await navigator.share({
                    title: title || "Share...",
                    text: text,
                    url: url,
                });
            } catch (error) {
                console.error("Share error: ", error);
            }
        } else {
            alert("Sharing functionality is not available in this browser.");
        }
    };

    return (
        <a href='/' className={'btn btn-secondary mr-3 mb-3'}
            onClick={handleShare}
            target={'_blank'} rel="noreferrer">
            <img src={`/static/img/social/share-more.svg`} alt={'More...'}
                width={'25px'} height={'25px'} />{' '} More..
        </a>
    );
};

const SocialShare = ({ url, text }) => {
    const platforms = [
        {
            name: 'facebook',
            appUrl: `https://www.facebook.com/sharer/sharer.php?u=${url}`,
            webUrl: `https://www.facebook.com/sharer/sharer.php?u=${url}`,
        },
        {
            name: 'linkedin',
            appUrl: `linkedin://shareArticle?mini=true&url=${url}&title=${text}`,
            webUrl: `https://www.linkedin.com/sharing/share-offsite/?url=${url}`,
        },
        {
            name: 'telegram',
            appUrl: `tg://msg_url?url=${url}&text=${text}`,
            webUrl: `https://t.me/share/url?url=${url}&text=${text}`,
        },
        {
            name: 'twitter',
            appUrl: `twitter://post?message=${text} ${url}`,
            webUrl: `https://twitter.com/intent/tweet?text=${text}&url=${url}`,
        },
        {
            name: 'whatsapp',
            appUrl: `whatsapp://send?text=${text} ${url}`,
            webUrl: `https://wa.me/?text=${text} ${url}`,
        },
        {
            name: 'wechat',
            appUrl: `https://api.qrserver.com/v1/create-qr-code/?size=154x154&data=${url}`,
            webUrl: `https://api.qrserver.com/v1/create-qr-code/?size=154x154&data=${url}`,
        },
    ];

    const openLink = (appUrl, webUrl) => {
        if (appUrl === webUrl) {
            window.open(webUrl, "_blank");
            return;
        }

        window.open(webUrl, "_blank");

        let appOpened = false;

        const iframe = document.createElement("iframe");
        iframe.style.display = "none";
        iframe.src = appUrl;

        iframe.onload = () => {
            appOpened = true;
            document.body.removeChild(iframe);
        };

        document.body.appendChild(iframe);

        setTimeout(() => {
            if (!appOpened) {
                window.open(webUrl, "_blank");
                document.body.removeChild(iframe);
            }
        }, 1000);
    };

    return (
        <>
            {platforms.map((platform) => (
                <>
                    <a key={platform.name} href={platform.webUrl} className={'btn btn-secondary mr-3 mb-3'}
                        onClick={(e) => { e.preventDefault(); openLink(platform.appUrl, platform.webUrl) }}
                        target={'_blank'} rel="noreferrer">
                        <img src={`/static/img/social/${platform.name}.svg`} alt={platform.name.charAt(0).toUpperCase() + platform.name.slice(1)}
                            width={'25px'} height={'25px'} />{' '}
                        {platform.name.charAt(0).toUpperCase() + platform.name.slice(1)}
                    </a>
                </>
            ))}
            <MoreShareButton url={decodeURIComponent(url)} text={decodeURIComponent(text)} />
        </>
    );
};

export default function RenderShareButtons({ shareLinks, itemPath, itemType }) {
    const share_text = encodeURIComponent('shared from sesamedisk.com');

    const sharedLinkInfo = shareLinks.length > 0 ? shareLinks[0] : null;
    const isFile = itemType === 'file';
    if (!sharedLinkInfo) return <>
        <div>{`Generate a link to share this ${isFile ? 'file' : 'folder'}`}</div>
        {
            isFile && genEmbedCode(itemPath, '') &&
            <FormGroup className="mb-0">
                <dt className="text-secondary font-weight-normal">{gettext('Embed code:')}</dt>
                <dd>Generate a link with download permissions to share this file as embed code</dd>
            </FormGroup>
        }
    </>;

    const canEmbedLinks = shareLinks.filter(l => l.permissions.can_download);

    let share_link = '';
    if (sharedLinkInfo) {
        share_link = sharedLinkInfo.link;
        if (sharedLinkInfo.permissions.can_download && !sharedLinkInfo.is_dir)
            share_link += '?raw=1';
    }
    const encoded_url = encodeURIComponent(share_link);

    return <>
        <div>
            <SocialShare url={encoded_url} text={share_text} />
        </div>
        {sharedLinkInfo && !sharedLinkInfo.is_dir && genEmbedCode(sharedLinkInfo.obj_name, '') &&
            <FormGroup className="mb-0">
                <dt className="text-secondary font-weight-normal">{gettext('Embed code:')}</dt>
                <dd>
                    {
                        canEmbedLinks.length > 0 ? <RenderEmbedCode sharedLinkInfo={canEmbedLinks[0]} /> :
                            'To share this file as embed code, you must create a link with download permission'
                    }
                </dd>
            </FormGroup>
        }
    </>;
}

export function RenderEmbedCode({ sharedLinkInfo }) {
    let share_link = sharedLinkInfo.link + '?raw=1';
    const password = sharedLinkInfo.password;

    if (password) {
        share_link = changeLinkToChina(share_link);
        share_link += '&ep=' + encodeURIComponent(btoa(password));
    }

    const onCopyCode = () => {
        copy(code);
        toaster.success(gettext('Embed code is copied to the clipboard.'));
    };

    const code = genEmbedCode(sharedLinkInfo.obj_name, share_link);
    if (!code) return null;

    return <div>
        <code>
            {code}
        </code>
        <InputGroupAddon addonType="append" className={'mt-2'}>
            <Button color="primary" onClick={onCopyCode} className="border-0">{gettext('Copy')}</Button>
        </InputGroupAddon>
    </div>;
}

function genEmbedCode(filename, link) {
    const ext = filename.split('.').pop().toLowerCase();
    switch (ext) {
        case 'jpg':
        case 'jpeg':
        case 'png':
        case 'gif':
        case 'bmp':
        case 'svg':
        case 'webp':
            return '<img src="' + link + '" width="400" height="auto" alt="Image description">';
        case 'mp4':
        case 'webm':
        case 'avi':
        case 'mkv':
        case 'flv':
        case 'mov':
            return '<video width="600" height="400" controls><source src="' + link + '" type="video/' + ext + '">Your browser does not support the video tag</video>';
        case 'mp3':
        case 'wav':
        case 'ogg':
        case 'aac':
        case 'flac':
        case 'wma':
            return '<audio controls><source src="' + link + '" type="audio/' + ext + '">Your browser does not support the video tag</audio>';
        case 'txt':
            return '<iframe src="' + link + '" width="600" height="400" frameborder="0" scrolling="auto"></iframe>';
        case 'pdf':
            return '<embed src="' + link + '" type="application/pdf" width="600" height="400">';
        case 'doc':
        case 'docx':
        case 'ppt':
        case 'pptx':
        case 'xls':
        case 'xlsx':
            return '<iframe src="https://view.officeapps.live.com/op/embed.aspx?src=' + encodeURIComponent(link) + '" width="600" height="400" frameborder="0" scrolling="no"></iframe>';
        default:
            return null;
    }
}
