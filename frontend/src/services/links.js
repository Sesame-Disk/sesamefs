/* eslint-disable */

const publicShareDomain = process.env.REACT_APP_SHARE_DOMAIN || 'https://app.nihaocloud.com';
const publicShareDomainChina = process.env.REACT_APP_SHARE_DOMAIN_CHINA || 'https://app.nihaoshares.com';

export function changeLinkToChina(input) {
    // If input is a string, replace and return the string
    if (typeof input === 'string') {
        return input.replace(publicShareDomain, publicShareDomainChina);
    }

    // If input is an object with a 'link' property, replace and return the object
    if (input && typeof input === 'object' && input.link) {
        let link = input.link;
        input.link = link.replace(publicShareDomain, publicShareDomainChina);
        return input;
    }

    // Return input unchanged if it doesn't match expected types
    return input;
}
