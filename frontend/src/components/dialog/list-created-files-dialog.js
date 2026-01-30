import React from 'react';
import PropTypes from 'prop-types';
import moment from 'moment';
import { Button, Table } from 'reactstrap';
import { gettext, siteRoot } from '../../utils/constants';
import { Utils } from '../../utils/utils';

const propTypes = {
  activity: PropTypes.object.isRequired,
  toggleCancel: PropTypes.func.isRequired,
};

class ListCreatedFileDialog extends React.Component {

  toggle = (activity) => {
    this.props.toggleCancel(activity);
  };

  render() {
    let activity = this.props.activity;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Created Files')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <Table>
            <thead>
              <tr>
                <th width='75%'>{gettext('Name')}</th>
                <th width='25%'>{gettext('Time')}</th>
              </tr>
            </thead>
            <tbody>
              {
                activity.createdFilesList.map((item, index) => {
                  let fileURL = `${siteRoot}lib/${item.repo_id}/file${Utils.encodePath(item.path)}`;
                  let fileLink = <a href={fileURL} target='_blank' rel="noreferrer">{item.name}</a>;
                  if (item.name.endsWith('(draft).md')) { // be compatible with the existing draft files
                    fileLink = item.name;
                  }
                  return (
                    <tr key={index}>
                      <td>{fileLink}</td>
                      <td>{moment(item.time).fromNow()}</td>
                    </tr>
                  );
                })
              }
            </tbody>
          </Table>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.toggle.bind(this, activity)}>{gettext('Close')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ListCreatedFileDialog.propTypes = propTypes;

export default ListCreatedFileDialog;
